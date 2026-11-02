package operations

import (
	"0xbot.org/api/internal/capabilities"
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/prices"
	"context"
	"errors"
	"testing"
)

type rwaRouterFake struct{ testRouter }

func (r *rwaRouterFake) RWATokenBalance(context.Context, data.EquityTokenRequest) (data.Balance, string, error) {
	r.calls++
	return data.Balance{Amount: "5"}, "test", r.err
}
func (r *rwaRouterFake) EquityPrice(context.Context, prices.EquityRequest) (prices.Quote, string, error) {
	r.calls++
	return prices.Quote{Price: "3194.56", Currency: "USD"}, "test", r.err
}

func equityInput(t *testing.T) data.EquityTokenRequest {
	t.Helper()
	for _, token := range data.Tokens() {
		if token.AssetClass == data.ClassTokenizedEquity {
			return data.EquityTokenRequest{Address: "0x0000000000000000000000000000000000000001", Network: token.Network, Contract: token.Contract}
		}
	}
	t.Fatal("no curated equity token")
	return data.EquityTokenRequest{}
}

func TestRWABalanceLimitAndMetering(t *testing.T) {
	router, recorder := &rwaRouterFake{}, &testRecorder{}
	service := New(router, recorder, &testLimiter{t, map[string]int{}})
	in := equityInput(t)
	bad := in
	bad.Contract = "0xdac17f958d2ee523a2206206994597c13d831ec7"
	if _, err := service.RWATokenBalance(context.Background(), "a", bad); !errors.Is(err, data.ErrInput) || router.calls != 0 {
		t.Fatal("invalid request reached provider")
	}
	for i := 0; i < 20; i++ {
		if _, err := service.RWATokenBalance(context.Background(), "a", in); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.RWATokenBalance(context.Background(), "a", in); !errors.Is(err, data.ErrRateLimited) {
		t.Fatal("equity balance limit not enforced")
	}
	if len(recorder.records) != 20 || recorder.records[0].Capability != capabilities.RWATokenBalance {
		t.Fatal("incorrect metering")
	}
	if _, err := service.RWATokenBalance(context.Background(), "b", in); err != nil {
		t.Fatal("limit must be per workspace")
	}
}

func TestEquityPriceSharesPriceBudget(t *testing.T) {
	router, recorder := &rwaRouterFake{}, &testRecorder{}
	limiter := &testLimiter{t, map[string]int{}}
	service := New(router, recorder, limiter)
	contract := equityInput(t).Contract
	in := prices.EquityRequest{Network: "eip155:1", Contract: contract}
	for i := 0; i < 20; i++ {
		if _, err := service.EquityPrice(context.Background(), "a", in); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.EquityPrice(context.Background(), "a", in); !errors.Is(err, data.ErrRateLimited) {
		t.Fatal("equity price must share the price-query budget")
	}
	if limiter.counts["price-query:a"] != 21 || len(recorder.records) != 20 {
		t.Fatal("price budget bookkeeping mismatch")
	}
	bad := in
	bad.Contract = "0xdac17f958d2ee523a2206206994597c13d831ec7"
	if _, err := service.EquityPrice(context.Background(), "a", bad); !errors.Is(err, data.ErrInput) || router.calls != 20 {
		t.Fatal("invalid equity price request reached provider")
	}
}
