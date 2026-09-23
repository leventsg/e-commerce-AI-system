//go:build checkout_internal_tests

package logic

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	model "github.com/leventsg/e-commerce-AI-system/dal/model/checkout"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/checkout"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/services/inventory/inventory"
	"github.com/leventsg/e-commerce-AI-system/services/inventory/inventoryclient"
	"github.com/leventsg/e-commerce-AI-system/services/product/product"
	"github.com/leventsg/e-commerce-AI-system/services/product/productcatalogservice"
	"github.com/leventsg/e-commerce-AI-system/tests/checkout/testutil"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"google.golang.org/grpc"
)

const requestJSON = `{"coupon_id":"","order_items":[{"product_id":42,"quantity":1}]}`

func requestHash() string {
	sum := sha256.Sum256([]byte(requestJSON))
	return hex.EncodeToString(sum[:])
}
func nullString(value string) sql.NullString { return sql.NullString{String: value, Valid: true} }
func tokenRequest(token string) *checkout.CheckoutReq {
	return &checkout.CheckoutReq{UserId: 7, CheckoutToken: token, OrderItems: []*checkout.CheckoutReq_OrderItem{{ProductId: 42, Quantity: 1}}}
}
func intentRow(state int64) *model.Checkouts {
	return &model.Checkouts{PreOrderId: "pre-1", UserId: 7, Status: 4, CheckoutToken: nullString(strings.Repeat("a", 64)), PrepareState: state, TokenExpiresAt: time.Now().Add(time.Minute).Unix(), PrepareStartedAt: time.Now().Unix()}
}
func testService(t *testing.T) (*svc.ServiceContext, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
		db.Close()
	})
	conn := sqlx.NewSqlConnFromDB(db)
	return &svc.ServiceContext{Mysql: conn, CheckoutIntents: model.NewCheckoutIntentStore(conn)}, mock
}
func expectFind(mock sqlmock.Sqlmock, row *model.Checkouts) {
	mock.ExpectQuery("FROM `checkouts` WHERE user_id = \\? AND checkout_token = \\? LIMIT 1").WithArgs(uint64(7), row.CheckoutToken.String).WillReturnRows(testutil.Rows(row))
}
func expectClaim(mock sqlmock.Sqlmock, token string, affected int64) {
	mock.ExpectExec("UPDATE `checkouts` SET checkout_request_hash = .*AND prepare_state = 0 AND token_expires_at > \\?").WithArgs(requestHash(), requestJSON, sqlmock.AnyArg(), uint64(7), token, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, affected))
}

func TestPrepareReplayAndTerminalStates(t *testing.T) {
	cases := []struct {
		name    string
		state   int64
		result  string
		expired bool
		hash    string
		want    int32
	}{
		{"success", 2, `{"pre_order_id":"pre-1","expire_time":999,"pay_method":[1,2]}`, false, requestHash(), 0},
		{"success_after_token_expiry", 2, `{"pre_order_id":"pre-1","expire_time":999,"pay_method":[1,2]}`, true, requestHash(), 0},
		{"failure", 3, `{"status_code":60010,"status_msg":"库存不足"}`, false, requestHash(), 60010},
		{"processing", 1, "", false, requestHash(), code.CheckoutProcessing},
		{"unknown", 4, "", false, requestHash(), code.CheckoutOutcomeUnknown},
		{"expired_unused", 0, "", true, "", code.CheckoutTokenExpired},
		{"changed_parameters", 2, `{}`, false, "different-hash", code.CheckoutTokenConflict},
		{"corrupt_success", 2, `{}`, false, requestHash(), code.CheckoutOutcomeUnknown},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			service, mock := testService(t)
			row := intentRow(test.state)
			if test.hash != "" {
				row.CheckoutRequestHash = nullString(test.hash)
			}
			if test.result != "" {
				row.PrepareResult = nullString(test.result)
			}
			if test.expired {
				row.TokenExpiresAt = 1
			}
			expectFind(mock, row)
			response, err := NewPrepareCheckoutLogic(context.Background(), service).PrepareCheckout(tokenRequest(row.CheckoutToken.String))
			if err != nil || response.StatusCode != test.want {
				t.Fatalf("response=%+v err=%v", response, err)
			}
			if test.want == 0 && (response.PreOrderId != "pre-1" || response.ExpireTime != 999 || len(response.PayMethod) != 2) {
				t.Fatalf("incomplete replay: %+v", response)
			}
			if test.want != 0 && response.PreOrderId != "" {
				t.Fatalf("must not advertise an unfinished preorder: %+v", response)
			}
		})
	}
}

func TestInvalidAndCrossUserTokenNeverCallBusiness(t *testing.T) {
	t.Run("missing token", func(t *testing.T) {
		response, err := NewPrepareCheckoutLogic(context.Background(), &svc.ServiceContext{}).PrepareCheckout(tokenRequest(""))
		if err != nil || response.StatusCode != code.InvalidParams {
			t.Fatalf("%+v %v", response, err)
		}
	})
	t.Run("cross user", func(t *testing.T) {
		service, mock := testService(t)
		token := strings.Repeat("a", 64)
		mock.ExpectQuery("WHERE user_id = \\? AND checkout_token = \\?").WithArgs(uint64(7), token).WillReturnRows(sqlmock.NewRows([]string{"pre_order_id"}))
		response, err := NewPrepareCheckoutLogic(context.Background(), service).PrepareCheckout(tokenRequest(token))
		if err != nil || response.StatusCode != code.CheckoutTokenInvalid {
			t.Fatalf("%+v %v", response, err)
		}
	})
}

func TestStaleProcessingIsNotReexecuted(t *testing.T) {
	service, mock := testService(t)
	row := intentRow(1)
	row.PrepareStartedAt = 1
	row.CheckoutRequestHash = nullString(requestHash())
	expectFind(mock, row)
	mock.ExpectExec("UPDATE `checkouts` SET prepare_state = .*AND prepare_state = 1").WithArgs(int64(4), sqlmock.AnyArg(), uint64(7), "pre-1").WillReturnResult(sqlmock.NewResult(0, 1))
	response, err := NewPrepareCheckoutLogic(context.Background(), service).PrepareCheckout(tokenRequest(row.CheckoutToken.String))
	if err != nil || response.StatusCode != code.CheckoutOutcomeUnknown {
		t.Fatalf("%+v %v", response, err)
	}
}

func TestConcurrentLoserReadsStateWithoutBusinessWrite(t *testing.T) {
	service, mock := testService(t)
	row := intentRow(0)
	expectFind(mock, row)
	expectClaim(mock, row.CheckoutToken.String, 0)
	row.PrepareState = 1
	row.CheckoutRequestHash = nullString(requestHash())
	expectFind(mock, row)
	response, err := NewPrepareCheckoutLogic(context.Background(), service).PrepareCheckout(tokenRequest(row.CheckoutToken.String))
	if err != nil || response.StatusCode != code.CheckoutProcessing {
		t.Fatalf("%+v %v", response, err)
	}
}

type productStub struct {
	productcatalogservice.ProductCatalogService
}

func (*productStub) GetProduct(_ context.Context, in *product.GetProductReq, _ ...grpc.CallOption) (*product.GetProductResp, error) {
	return &product.GetProductResp{Product: &product.Product{Id: in.Id, Name: "Phone", Price: 100}}, nil
}

type inventoryStub struct {
	inventoryclient.Inventory
	calls   atomic.Int32
	outcome error
	status  int32
}

func (f *inventoryStub) DecreasePreInventory(ctx context.Context, in *inventory.InventoryReq, _ ...grpc.CallOption) (*inventory.InventoryResp, error) {
	f.calls.Add(1)
	if _, ok := ctx.Deadline(); !ok {
		return nil, errors.New("unbounded inventory call")
	}
	if in.UserId != 7 || len(in.Items) != 1 || in.Items[0].ProductId != 42 || in.Items[0].Quantity != 1 {
		return nil, errors.New("wrong trusted inventory arguments")
	}
	return &inventory.InventoryResp{StatusCode: f.status}, f.outcome
}

type scheduleHook struct {
	t   *testing.T
	err error
}

func (h scheduleHook) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h scheduleHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (h scheduleHook) ProcessHook(_ redis.ProcessHook) redis.ProcessHook {
	return func(_ context.Context, cmd redis.Cmder) error {
		if cmd.Name() != "zadd" || len(cmd.Args()) != 4 || cmd.Args()[1] != biz.CheckoutTimeoutZSetKey {
			return fmt.Errorf("unexpected redis command: %v", cmd.Args())
		}
		if h.err != nil {
			return h.err
		}
		cmd.(*redis.IntCmd).SetVal(1)
		return nil
	}
}

var redisTestID atomic.Int64

func scheduleRedis(t *testing.T, err error) *redis.Redis {
	t.Helper()
	r, e := redis.NewRedis(redis.RedisConf{Host: fmt.Sprintf("checkout-test-%d:6379", redisTestID.Add(1)), Type: redis.NodeType, NonBlock: true}, redis.WithHook(scheduleHook{t, err}))
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func expectComplete(mock sqlmock.Sqlmock, pre string, commitErr error) {
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `checkouts` SET coupon_id = .*AND prepare_state = 1").WithArgs(nil, int64(100), int64(100), sqlmock.AnyArg(), sqlmock.AnyArg(), uint64(7), pre).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("insert into `checkout_items`").WithArgs(pre, uint64(42), uint64(1), int64(100), `{"desc":"","name":"Phone"}`).WillReturnResult(sqlmock.NewResult(0, 1))
	commit := mock.ExpectCommit()
	if commitErr != nil {
		commit.WillReturnError(commitErr)
	}
}

func TestNewTokensCanBuyIdenticalItems(t *testing.T) {
	service, mock := testService(t)
	inv := &inventoryStub{}
	service.InventoryRpc = inv
	service.ProductRpc = &productStub{}
	service.RedisClient = scheduleRedis(t, nil)
	for i, letter := range []string{"a", "b"} {
		row := intentRow(0)
		row.CheckoutToken = nullString(strings.Repeat(letter, 64))
		row.PreOrderId = fmt.Sprintf("pre-%d", i+1)
		expectFind(mock, row)
		expectClaim(mock, row.CheckoutToken.String, 1)
		expectComplete(mock, row.PreOrderId, nil)
		response, err := NewPrepareCheckoutLogic(context.Background(), service).PrepareCheckout(tokenRequest(row.CheckoutToken.String))
		if err != nil || response.StatusCode != 0 || response.PreOrderId != row.PreOrderId || response.ExpireTime <= time.Now().Unix() {
			t.Fatalf("independent purchase failed: %+v %v", response, err)
		}
	}
	if inv.calls.Load() != 2 {
		t.Fatalf("new intents must reserve independently: %d", inv.calls.Load())
	}
}

func TestInventoryTimeoutIsPersistedAsUnknown(t *testing.T) {
	service, mock := testService(t)
	inv := &inventoryStub{outcome: context.DeadlineExceeded}
	service.InventoryRpc = inv
	service.ProductRpc = &productStub{}
	service.RedisClient = scheduleRedis(t, nil)
	row := intentRow(0)
	expectFind(mock, row)
	expectClaim(mock, row.CheckoutToken.String, 1)
	mock.ExpectExec("UPDATE `checkouts` SET prepare_state = .*AND prepare_state = 1").WithArgs(int64(4), sqlmock.AnyArg(), uint64(7), "pre-1").WillReturnResult(sqlmock.NewResult(0, 1))
	response, err := NewPrepareCheckoutLogic(context.Background(), service).PrepareCheckout(tokenRequest(row.CheckoutToken.String))
	if err != nil || response.StatusCode != code.CheckoutOutcomeUnknown || inv.calls.Load() != 1 {
		t.Fatalf("%+v %v calls=%d", response, err, inv.calls.Load())
	}
	row.PrepareState = 4
	row.CheckoutRequestHash = nullString(requestHash())
	expectFind(mock, row)
	response, err = NewPrepareCheckoutLogic(context.Background(), service).PrepareCheckout(tokenRequest(row.CheckoutToken.String))
	if err != nil || response.StatusCode != code.CheckoutOutcomeUnknown || inv.calls.Load() != 1 {
		t.Fatalf("retry must not reserve again: %+v %v", response, err)
	}
}

func TestSchedulingFailureNeverReservesInventory(t *testing.T) {
	service, mock := testService(t)
	inv := &inventoryStub{}
	service.InventoryRpc = inv
	service.ProductRpc = &productStub{}
	service.RedisClient = scheduleRedis(t, errors.New("scheduler unavailable"))
	row := intentRow(0)
	expectFind(mock, row)
	expectClaim(mock, row.CheckoutToken.String, 1)
	mock.ExpectExec("UPDATE `checkouts` SET prepare_state = .*AND prepare_state = 1").WithArgs(int64(3), sqlmock.AnyArg(), uint64(7), "pre-1").WillReturnResult(sqlmock.NewResult(0, 1))
	response, err := NewPrepareCheckoutLogic(context.Background(), service).PrepareCheckout(tokenRequest(row.CheckoutToken.String))
	if err != nil || response.StatusCode != code.InternalFailed || inv.calls.Load() != 0 {
		t.Fatalf("scheduling failure must precede reservation: %+v %v calls=%d", response, err, inv.calls.Load())
	}
}

func TestInventoryRejectionPersistsFailureWithoutCheckout(t *testing.T) {
	service, mock := testService(t)
	inv := &inventoryStub{status: code.InventoryNotEnough}
	service.InventoryRpc = inv
	service.ProductRpc = &productStub{}
	service.RedisClient = scheduleRedis(t, nil)
	row := intentRow(0)
	expectFind(mock, row)
	expectClaim(mock, row.CheckoutToken.String, 1)
	mock.ExpectExec("UPDATE `checkouts` SET prepare_state = .*AND prepare_state = 1").WithArgs(int64(3), sqlmock.AnyArg(), uint64(7), "pre-1").WillReturnResult(sqlmock.NewResult(0, 1))
	response, err := NewPrepareCheckoutLogic(context.Background(), service).PrepareCheckout(tokenRequest(row.CheckoutToken.String))
	if err != nil || response.StatusCode != code.InventoryNotEnough || response.PreOrderId != "" {
		t.Fatalf("must not create a checkout after inventory rejection: %+v %v", response, err)
	}
}

func TestUnconfirmedCommitNeverRollsBackOrReexecutes(t *testing.T) {
	service, mock := testService(t)
	inv := &inventoryStub{}
	service.InventoryRpc = inv
	service.ProductRpc = &productStub{}
	service.RedisClient = scheduleRedis(t, nil)
	row := intentRow(0)
	expectFind(mock, row)
	expectClaim(mock, row.CheckoutToken.String, 1)
	expectComplete(mock, row.PreOrderId, errors.New("lost commit acknowledgement"))
	row.PrepareState = 1
	row.CheckoutRequestHash = nullString(requestHash())
	expectFind(mock, row)
	mock.ExpectExec("UPDATE `checkouts` SET prepare_state = .*AND prepare_state = 1").WithArgs(int64(4), sqlmock.AnyArg(), uint64(7), "pre-1").WillReturnResult(sqlmock.NewResult(0, 1))
	response, err := NewPrepareCheckoutLogic(context.Background(), service).PrepareCheckout(tokenRequest(row.CheckoutToken.String))
	if err != nil || response.StatusCode != code.CheckoutOutcomeUnknown || inv.calls.Load() != 1 {
		t.Fatalf("ambiguous commit must remain uncertain: %+v %v", response, err)
	}
}

func TestLostCommitAcknowledgementRecoversSuccess(t *testing.T) {
	service, mock := testService(t)
	inv := &inventoryStub{}
	service.InventoryRpc = inv
	service.ProductRpc = &productStub{}
	service.RedisClient = scheduleRedis(t, nil)
	row := intentRow(0)
	expectFind(mock, row)
	expectClaim(mock, row.CheckoutToken.String, 1)
	expectComplete(mock, row.PreOrderId, errors.New("lost commit acknowledgement"))
	row.PrepareState = 2
	row.CheckoutRequestHash = nullString(requestHash())
	row.PrepareResult = nullString(`{"pre_order_id":"pre-1","expire_time":999,"pay_method":[1,2]}`)
	expectFind(mock, row)
	response, err := NewPrepareCheckoutLogic(context.Background(), service).PrepareCheckout(tokenRequest(row.CheckoutToken.String))
	if err != nil || response.StatusCode != 0 || response.PreOrderId != "pre-1" || inv.calls.Load() != 1 {
		t.Fatalf("%+v %v", response, err)
	}
}

func TestConcurrentClaimsHaveOneWinner(t *testing.T) {
	service, mock := testService(t)
	mock.MatchExpectationsInOrder(false)
	for i := 0; i < 16; i++ {
		affected := int64(0)
		if i == 0 {
			affected = 1
		}
		expectClaim(mock, strings.Repeat("a", 64), affected)
	}
	var winners atomic.Int32
	var group sync.WaitGroup
	for i := 0; i < 16; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			claimed, err := service.CheckoutIntents.Claim(context.Background(), 7, strings.Repeat("a", 64), requestHash(), requestJSON, time.Now().Unix())
			if err != nil {
				t.Error(err)
			}
			if claimed {
				winners.Add(1)
			}
		}()
	}
	group.Wait()
	if winners.Load() != 1 {
		t.Fatalf("expected one claimant, got %d", winners.Load())
	}
}

func TestParameterFingerprintNormalizesOrderButRejectsDuplicates(t *testing.T) {
	request := tokenRequest(strings.Repeat("a", 64))
	request.OrderItems = []*checkout.CheckoutReq_OrderItem{{ProductId: 43, Quantity: 2}, {ProductId: 42, Quantity: 1}}
	_, payload, _, err := normalizeCheckout(request)
	if err != nil || payload != `{"coupon_id":"","order_items":[{"product_id":42,"quantity":1},{"product_id":43,"quantity":2}]}` {
		t.Fatalf("unexpected canonical payload %s: %v", payload, err)
	}
	request.OrderItems[0].ProductId = 42
	if _, _, _, err := normalizeCheckout(request); err == nil {
		t.Fatal("duplicate product IDs must be rejected before inventory")
	}
}
