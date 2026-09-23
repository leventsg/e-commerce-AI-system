package checkout_test

import (
	"context"
	"database/sql"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"
	model "github.com/leventsg/e-commerce-AI-system/dal/model/checkout"
	"github.com/leventsg/e-commerce-AI-system/tests/checkout/testutil"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"testing"
)

func TestOnlyOneClaimMayConsumeToken(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := model.NewCheckoutIntentStore(sqlx.NewSqlConnFromDB(db))
	query := "UPDATE `checkouts` SET checkout_request_hash = \\?, checkout_request = \\?, prepare_state = 1, prepare_started_at = \\? WHERE user_id = \\? AND checkout_token = \\? AND prepare_state = 0 AND token_expires_at > \\?"
	for _, affected := range []int64{1, 0} {
		mock.ExpectExec(query).WithArgs("hash", `{"order_items":[]}`, int64(100), uint64(7), "token", int64(100)).WillReturnResult(sqlmock.NewResult(0, affected))
		claimed, err := store.Claim(context.Background(), 7, "token", "hash", `{"order_items":[]}`, 100)
		if err != nil || claimed != (affected == 1) {
			t.Fatalf("claimed=%v err=%v", claimed, err)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestIssuanceRetryReturnsPersistedToken(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := model.NewCheckoutIntentStore(sqlx.NewSqlConnFromDB(db))
	original := &model.Checkouts{UserId: 7, PreOrderId: "original-pre", CheckoutToken: sql.NullString{String: "original-token", Valid: true}, CheckoutIntentId: sql.NullString{String: "intent-1", Valid: true}, TokenExpiresAt: 123}
	mock.ExpectExec("insert into `checkouts`").WillReturnError(&mysql.MySQLError{Number: 1062})
	mock.ExpectQuery("where `user_id` = \\? and `checkout_intent_id` = \\?").WithArgs(uint64(7), "intent-1").WillReturnRows(testutil.Rows(original))
	result, err := store.Issue(context.Background(), &model.Checkouts{UserId: 7, CheckoutToken: sql.NullString{String: "replacement-token", Valid: true}, CheckoutIntentId: original.CheckoutIntentId})
	if err != nil || result.CheckoutToken.String != "original-token" || result.TokenExpiresAt != 123 {
		t.Fatalf("must preserve original token and expiry: %+v %v", result, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCompletionRollsBackAllSnapshotsOnFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := model.NewCheckoutIntentStore(sqlx.NewSqlConnFromDB(db))
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `checkouts` SET .*prepare_state = 2.*WHERE user_id = \\? AND pre_order_id = \\? AND prepare_state = 1").WithArgs(nil, int64(300), int64(300), int64(999), "response", uint64(7), "pre-1").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("insert into `checkout_items`").WithArgs("pre-1", uint64(42), uint64(1), int64(100), "{}").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("insert into `checkout_items`").WithArgs("pre-1", uint64(43), uint64(1), int64(200), "{}").WillReturnError(errors.New("write failed"))
	mock.ExpectRollback()
	err = store.Complete(context.Background(), &model.Checkouts{UserId: 7, PreOrderId: "pre-1", OriginalAmount: 300, FinalAmount: 300, ExpireTime: 999}, []*model.CheckoutItems{{PreOrderId: "pre-1", ProductId: 42, Quantity: 1, Price: 100, Snapshot: "{}"}, {PreOrderId: "pre-1", ProductId: 43, Quantity: 1, Price: 200, Snapshot: "{}"}}, "response")
	if err == nil {
		t.Fatal("partial checkout must not commit")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestIncompleteIntentCannotBeUsedForOrder(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m := model.NewCheckoutsModel(sqlx.NewSqlConnFromDB(db))
	mock.ExpectQuery("WHERE `user_id` = \\? AND `pre_order_id` = \\? AND `prepare_state` = 2 LIMIT 1").WithArgs(int32(7), "pre-1").WillReturnRows(sqlmock.NewRows([]string{"pre_order_id"}))
	_, err = m.FindOneByUserIdAndPreOrderId(context.Background(), 7, "pre-1")
	if !errors.Is(err, sqlx.ErrNotFound) {
		t.Fatalf("incomplete intent must not be visible: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLateUnknownCannotOverwriteSucceededResult(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := model.NewCheckoutIntentStore(sqlx.NewSqlConnFromDB(db))
	mock.ExpectExec("UPDATE `checkouts` SET prepare_state = \\?, prepare_result = \\? WHERE user_id = \\? AND pre_order_id = \\? AND prepare_state = 1").WithArgs(int64(4), "{}", uint64(7), "pre-1").WillReturnResult(sqlmock.NewResult(0, 0))
	if err := store.FinishFailure(context.Background(), 7, "pre-1", 4, "{}"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
