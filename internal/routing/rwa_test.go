package routing

import (
	"0xbot.org/api/internal/capabilities"
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/prices"
	"context"
	"testing"
)

type rwaFake struct {
	id    string
	fail  bool
	calls int
}

func (p *rwaFake) ID() string                   { return p.id }
func (p *rwaFake) Supports(network string) bool { return network == "eip155:1" }
func (p *rwaFake) CapabilityIDs() []string {
	return []string{capabilities.RWATokenBalance, capabilities.EquityPrice}
}
func (p *rwaFake) RWATokenBalance(context.Context, data.Request) (data.Balance, error) {
	p.calls++
	if p.fail {
		return data.Balance{}, data.ErrUnavailable
	}
	return data.Balance{Amount: "5"}, nil
}
func (p *rwaFake) EquityPrice(context.Context, prices.EquityRequest) (prices.Quote, error) {
	p.calls++
	if p.fail {
		return prices.Quote{}, data.ErrUnavailable
	}
	return prices.Quote{Price: "3194.56", Currency: "USD"}, nil
}

func equityContract(t *testing.T) string {
	t.Helper()
	for _, token := range data.Tokens() {
		if token.AssetClass == data.ClassTokenizedEquity {
			return token.Contract
		}
	}
	t.Fatal("no curated equity token")
	return ""
}

// Providers without the declared capabilities must never receive RWA calls,
// even when they rank first for the network.
func TestRWACapabilityEligibilityAndFallback(t *testing.T) {
	plain := &providerFake{id: "plain"}
	a, b := &rwaFake{id: "rwa-a", fail: true}, &rwaFake{id: "rwa-b"}
	r := New([]data.Provider{plain, a, b}, settingsFake{
		{ID: "plain", Enabled: true, Priority: 1},
		{ID: "rwa-a", Enabled: true, Priority: 2},
		{ID: "rwa-b", Enabled: true, Priority: 3},
	})
	contract := equityContract(t)
	in := data.EquityTokenRequest{Address: "0x0000000000000000000000000000000000000001", Network: "eip155:1", Contract: contract}
	balance, provider, e := r.RWATokenBalance(context.Background(), in)
	if e != nil || provider != "rwa-b" || balance.Amount != "5" || plain.calls != 0 || a.calls != 1 || b.calls != 1 {
		t.Fatalf("equity balance routing: %v %s plain=%d a=%d b=%d err=%v", balance, provider, plain.calls, a.calls, b.calls, e)
	}
	price, provider, e := r.EquityPrice(context.Background(), prices.EquityRequest{Network: "eip155:1", Contract: contract})
	if e != nil || provider != "rwa-b" || price.Price != "3194.56" || b.calls != 2 {
		t.Fatalf("equity price routing: %v %s err=%v", price, provider, e)
	}
	if _, _, e := r.RWATokenBalance(context.Background(), data.EquityTokenRequest{Address: "0x0000000000000000000000000000000000000001", Network: "eip155:1", Contract: "0xdac17f958d2ee523a2206206994597c13d831ec7"}); e != data.ErrInput {
		t.Fatal("stablecoin accepted by equity capability")
	}
}
