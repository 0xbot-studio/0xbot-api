package operations

import (
	"context"
	"errors"
	"testing"
	"time"

	"0xbot.org/api/internal/data"
)

type testRouter struct {
	calls int
	err   error
}

func (r *testRouter) Balance(context.Context, data.Request) (data.Balance, string, error) {
	return data.Balance{}, "test", r.err
}
func (r *testRouter) Balances(context.Context, data.Request) ([]data.Balance, string, error) {
	r.calls++
	return make([]data.Balance, 3), "test", r.err
}

type testRecorder struct{ records []Record }

func (r *testRecorder) Record(_ context.Context, record Record) error {
	r.records = append(r.records, record)
	return nil
}

type testLimiter struct {
	t      *testing.T
	counts map[string]int
}

func (l *testLimiter) Allow(_ context.Context, key string, limit int, period time.Duration) (bool, error) {
	if limit != 20 || period != time.Minute {
		l.t.Fatal("incorrect batch limit")
	}
	l.counts[key]++
	return l.counts[key] <= limit, nil
}
func TestBatchLimitAndSingleRecord(t *testing.T) {
	router, recorder := &testRouter{}, &testRecorder{}
	service := New(router, recorder, &testLimiter{t, map[string]int{}})
	req := data.Request{Address: "0x0000000000000000000000000000000000000001", Network: "eip155:1"}
	for i := 0; i < 20; i++ {
		result, err := service.Balances(context.Background(), "workspace-a", req)
		if err != nil || len(result.Data) != 3 {
			t.Fatalf("batch: %v", err)
		}
	}
	if _, err := service.Balances(context.Background(), "workspace-a", req); !errors.Is(err, data.ErrRateLimited) {
		t.Fatalf("limit: %v", err)
	}
	if router.calls != 20 || len(recorder.records) != 20 {
		t.Fatal("batch must meter once; rejected requests must not reach upstream")
	}
	router.err = data.ErrUnavailable
	result, err := service.Balances(context.Background(), "workspace-b", req)
	if !errors.Is(err, data.ErrUnavailable) || result.Data != nil {
		t.Fatal("failure must not expose partial data")
	}
	if len(recorder.records) != 21 {
		t.Fatal("separate workspace must have its own limit")
	}
	last := recorder.records[20]
	if last.Capability != "data.balances" || last.Status != "failed" || last.WorkspaceID != "workspace-b" {
		t.Fatalf("incorrect failure record: %+v", last)
	}
}
