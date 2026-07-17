package confirmations

import (
	"errors"
	"testing"
)

func TestConditionalUpdateResultUsesCommittedRowsWhenCacheInvalidationFails(t *testing.T) {
	updated, err := conditionalUpdateResult(fakeSQLResult{rows: 1}, errors.New("cache unavailable"))
	if err != nil || !updated {
		t.Fatalf("updated=%v err=%v, want committed update", updated, err)
	}
}

func TestConditionalUpdateResultReturnsDatabaseErrorWithoutResult(t *testing.T) {
	databaseErr := errors.New("database unavailable")
	updated, err := conditionalUpdateResult(nil, databaseErr)
	if updated || !errors.Is(err, databaseErr) {
		t.Fatalf("updated=%v err=%v", updated, err)
	}
}

func TestConditionalUpdateResultReportsCASMiss(t *testing.T) {
	updated, err := conditionalUpdateResult(fakeSQLResult{rows: 0}, nil)
	if err != nil || updated {
		t.Fatalf("updated=%v err=%v, want CAS miss", updated, err)
	}
}

type fakeSQLResult struct {
	rows int64
}

func (r fakeSQLResult) LastInsertId() (int64, error) { return 0, nil }
func (r fakeSQLResult) RowsAffected() (int64, error) { return r.rows, nil }
