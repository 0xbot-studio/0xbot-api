package operations

import (
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/swaps"
	"context"
	"testing"
	"time"
)

type swapRouter struct{ testRouter }

func (r *swapRouter) SwapTokens(context.Context, swaps.TokensInput) ([]swaps.Token, string, error) {
	r.calls++
	return []swaps.Token{}, "lifi", r.err
}

type swapLimit map[string]int

func (l swapLimit) Allow(_ context.Context, k string, n int, d time.Duration) (bool, error) {
	if n != 30 || d != time.Minute {
		panic("incorrect swap limit")
	}
	l[k]++
	return l[k] <= n, nil
}
func TestSwapWorkspaceLimitAndMetering(t *testing.T) {
	router, recorder := &swapRouter{}, &testRecorder{}
	s := New(router, recorder, swapLimit{})
	in := swaps.TokensInput{Network: "eip155:1"}
	for i := 0; i < 30; i++ {
		if _, e := s.SwapTokens(context.Background(), "a", in); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := s.SwapTokens(context.Background(), "a", in); e != data.ErrRateLimited {
		t.Fatal(e)
	}
	if _, e := s.SwapTokens(context.Background(), "", in); e != data.ErrInput {
		t.Fatal(e)
	}
	router.err = data.ErrUnavailable
	if _, e := s.SwapTokens(context.Background(), "b", in); e != data.ErrUnavailable {
		t.Fatal(e)
	}
	if router.calls != 31 || len(recorder.records) != 31 {
		t.Fatal("query was retried or double metered")
	}
	last := recorder.records[30]
	if last.WorkspaceID != "b" || last.Status != "failed" || last.Provider != "lifi" || last.Capability != "swaps.tokens" {
		t.Fatal(last)
	}
}
