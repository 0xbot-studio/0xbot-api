package routing

import (
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/prices"
	"context"
	"errors"
	"testing"
)

type priceFake struct {
	id    string
	err   error
	calls int
}

func (p *priceFake) ID() string             { return p.id }
func (p *priceFake) Supports(n string) bool { return n == "market" || n == "eip155:1" }
func (p *priceFake) CapabilityIDs() []string {
	return []string{"market.asset_price", "market.token_price"}
}
func (p *priceFake) AssetPrice(context.Context, prices.AssetRequest) (prices.Quote, error) {
	p.calls++
	return prices.Quote{Price: "1"}, p.err
}
func (p *priceFake) TokenPrice(context.Context, prices.TokenRequest) (prices.Quote, error) {
	p.calls++
	return prices.Quote{Price: "1"}, p.err
}
func TestPriceRouting(t *testing.T) {
	a, b, legacy := &priceFake{id: "a", err: data.ErrUnavailable}, &priceFake{id: "b"}, &providerFake{id: "legacy"}
	r := New([]data.Provider{legacy, a, b}, settingsFake{{"legacy", true, 0}, {"a", true, 1}, {"b", true, 2}})
	in := prices.AssetRequest{Asset: "BTC"}
	if _, p, e := r.AssetPrice(context.Background(), in); e != nil || p != "b" || legacy.calls != 0 {
		t.Fatal(p, e)
	}
	in.Provider = "legacy"
	if _, _, e := r.AssetPrice(context.Background(), in); !errors.Is(e, data.ErrProvider) {
		t.Fatal(e)
	}
	in.Provider = "a"
	if _, _, e := r.AssetPrice(context.Background(), in); !errors.Is(e, data.ErrUnavailable) || b.calls != 1 {
		t.Fatal("explicit fallback")
	}
	a.err = prices.ErrStale
	in.Provider = "auto"
	for i := 0; i < 4; i++ {
		if _, _, e := r.AssetPrice(context.Background(), in); !errors.Is(e, prices.ErrStale) {
			t.Fatal(e)
		}
	}
	if r.Health("a") != "healthy" || b.calls != 1 {
		t.Fatal("semantic price error changed source")
	}
	if _, _, e := r.TokenPrice(context.Background(), prices.TokenRequest{Network: "eip155:137", Contract: "0x0000000000000000000000000000000000000001"}); !errors.Is(e, data.ErrUnavailable) {
		t.Fatal(e)
	}
}
