package inventory

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

func TestTryLockOrderAcquiresOnSuccessfulInsert(t *testing.T) {
	model := &customInventoryModel{}
	session := execSession{execCtx: func(_ context.Context, query string, args ...any) (sql.Result, error) {
		if query != "INSERT INTO `inventory_lock` (order_id, user_id) VALUES (?, ?)" {
			t.Fatalf("query = %q", query)
		}
		if len(args) != 2 || args[0] != "pre-1" || args[1] != int64(42) {
			t.Fatalf("args = %#v", args)
		}
		return driver.ResultNoRows, nil
	}}

	acquired, err := model.TryLockOrder(context.Background(), session, "pre-1", 42, "`inventory_lock`")

	if err != nil || !acquired {
		t.Fatalf("acquired=%v err=%v", acquired, err)
	}
}

func TestTryLockOrderTreatsDuplicateKeyAsAlreadyAcquired(t *testing.T) {
	model := &customInventoryModel{}
	session := execSession{execCtx: func(context.Context, string, ...any) (sql.Result, error) {
		return nil, &mysql.MySQLError{Number: 1062, Message: "Duplicate entry"}
	}}

	acquired, err := model.TryLockOrder(context.Background(), session, "pre-1", 42, "`inventory_lock`")

	if err != nil || acquired {
		t.Fatalf("acquired=%v err=%v", acquired, err)
	}
}

func TestTryLockOrderReturnsNonDuplicateDatabaseErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "deadlock", err: &mysql.MySQLError{Number: 1213, Message: "deadlock"}},
		{name: "lock wait timeout", err: &mysql.MySQLError{Number: 1205, Message: "lock wait timeout"}},
		{name: "generic", err: errors.New("database unavailable")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := &customInventoryModel{}
			session := execSession{execCtx: func(context.Context, string, ...any) (sql.Result, error) {
				return nil, tt.err
			}}

			acquired, err := model.TryLockOrder(context.Background(), session, "pre-1", 42, "`inventory_lock`")

			if acquired || !errors.Is(err, tt.err) {
				t.Fatalf("acquired=%v err=%v, want %v", acquired, err, tt.err)
			}
		})
	}
}

type execSession struct {
	sqlx.Session
	execCtx func(context.Context, string, ...any) (sql.Result, error)
}

func (s execSession) ExecCtx(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return s.execCtx(ctx, query, args...)
}
