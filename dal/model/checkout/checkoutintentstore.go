package checkout

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-sql-driver/mysql"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

const (
	PrepareIssued int64 = iota
	PrepareProcessing
	PrepareSucceeded
	PrepareFailed
	PrepareUnknown
)

// CheckoutIntentStore owns durable intent transitions on checkouts. Redis is not
// consulted: losing a cache entry must never authorize another inventory write.
type CheckoutIntentStore struct{ conn sqlx.SqlConn }

func NewCheckoutIntentStore(conn sqlx.SqlConn) *CheckoutIntentStore {
	return &CheckoutIntentStore{conn: conn}
}

// Issue atomically creates an intent. A retried issuance returns the original
// token (including its original expiry); it never extends or replaces the intent.
func (s *CheckoutIntentStore) Issue(ctx context.Context, row *Checkouts) (*Checkouts, error) {
	_, err := NewCheckoutsModel(s.conn).Insert(ctx, row)
	if err != nil {
		var duplicate *mysql.MySQLError
		if !errors.As(err, &duplicate) || duplicate.Number != 1062 {
			return nil, err
		}
	}
	return NewCheckoutsModel(s.conn).FindOneByUserIdCheckoutIntentId(ctx, row.UserId, row.CheckoutIntentId)
}

func (s *CheckoutIntentStore) Find(ctx context.Context, userID uint64, token string) (*Checkouts, error) {
	var row Checkouts
	err := s.conn.QueryRowCtx(ctx, &row, "SELECT "+checkoutsRows+" FROM `checkouts` WHERE user_id = ? AND checkout_token = ? LIMIT 1", userID, token)
	return &row, err
}

func (s *CheckoutIntentStore) Claim(ctx context.Context, userID uint64, token, hash, request string, now int64) (bool, error) {
	result, err := s.conn.ExecCtx(ctx, "UPDATE `checkouts` SET checkout_request_hash = ?, checkout_request = ?, prepare_state = 1, prepare_started_at = ? WHERE user_id = ? AND checkout_token = ? AND prepare_state = 0 AND token_expires_at > ?", hash, request, now, userID, token, now)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected == 1, err
}

// FinishFailure only changes an active execution, never a committed success.
// Unknown outcomes are terminal for automated execution and require reconciliation.
func (s *CheckoutIntentStore) FinishFailure(ctx context.Context, userID uint64, preOrderID string, state int64, response string) error {
	if state != PrepareFailed && state != PrepareUnknown {
		return errors.New("invalid terminal prepare state")
	}
	_, err := s.conn.ExecCtx(ctx, "UPDATE `checkouts` SET prepare_state = ?, prepare_result = ? WHERE user_id = ? AND pre_order_id = ? AND prepare_state = 1", state, response, userID, preOrderID)
	return err
}

// Complete commits all item snapshots, prices and the replay response together.
// A commit error is ambiguous to the caller and must not trigger inventory rollback.
func (s *CheckoutIntentStore) Complete(ctx context.Context, row *Checkouts, items []*CheckoutItems, response string) error {
	return s.conn.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		result, err := session.ExecCtx(ctx, "UPDATE `checkouts` SET coupon_id = ?, original_amount = ?, final_amount = ?, status = 0, expire_time = ?, prepare_state = 2, prepare_result = ? WHERE user_id = ? AND pre_order_id = ? AND prepare_state = 1", row.CouponId, row.OriginalAmount, row.FinalAmount, row.ExpireTime, response, row.UserId, row.PreOrderId)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			return fmt.Errorf("checkout intent no longer processing")
		}
		model := NewCheckoutItemsModel(sqlx.NewSqlConnFromSession(session))
		for _, item := range items {
			if _, err := model.Insert(ctx, item); err != nil {
				return err
			}
		}
		return nil
	})
}
