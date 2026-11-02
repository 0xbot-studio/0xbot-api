package operations

import (
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/prices"
	"context"
	"errors"
	"testing"
)

type priceRouterFake struct{ testRouter }

func (r *priceRouterFake) AssetPrice(context.Context, prices.AssetRequest) (prices.Quote, string, error) {
	r.calls++
	return prices.Quote{Price: "1"}, "test", r.err
}
func (r *priceRouterFake) TokenPrice(context.Context, prices.TokenRequest) (prices.Quote, string, error) {
	r.calls++
	return prices.Quote{Price: "1"}, "test", r.err
}
func TestPriceQuotaRecords(t *testing.T) {
	r, rec := &priceRouterFake{}, &testRecorder{}
	s := New(r, rec, &testLimiter{t, map[string]int{}})
	ctx := context.Background()
	in := prices.AssetRequest{Asset: "BTC"}
	if _, e := s.AssetPrice(ctx, "", in); !errors.Is(e, data.ErrInput) {
		t.Fatal(e)
	}
	if _, e := s.AssetPrice(ctx, "a", prices.AssetRequest{Asset: "WBTC"}); !errors.Is(e, data.ErrInput) || r.calls != 0 {
		t.Fatal(e)
	}
	for i := 0; i < 10; i++ {
		if _, e := s.AssetPrice(ctx, "a", in); e != nil {
			t.Fatal(e)
		}
		if _, e := s.TokenPrice(ctx, "a", prices.TokenRequest{Network: "eip155:1", Contract: "0x0000000000000000000000000000000000000001"}); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := s.AssetPrice(ctx, "a", in); !errors.Is(e, data.ErrRateLimited) || len(rec.records) != 20 {
		t.Fatal(e)
	}
	r.err = prices.ErrStale
	q, e := s.AssetPrice(ctx, "b", in)
	if !errors.Is(e, prices.ErrStale) || q.Data.Price != "" {
		t.Fatal(q, e)
	}
	last := rec.records[20]
	if last.WorkspaceID != "b" || last.Capability != "market.asset_price" || last.ErrorCode != "price_stale" {
		t.Fatal(last)
	}
}
