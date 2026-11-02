package routing

import (
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/swaps"
	"context"
	"testing"
)

type swapFake struct {
	calls int
	err   error
}

func (*swapFake) ID() string             { return "lifi" }
func (*swapFake) Supports(n string) bool { return swaps.Chain(n) != "" }
func (*swapFake) CapabilityIDs() []string {
	return []string{"swaps.tokens", "swaps.quote", "swaps.status"}
}
func (p *swapFake) Tokens(context.Context, swaps.TokensInput) ([]swaps.Token, error) {
	p.calls++
	return []swaps.Token{}, p.err
}
func (p *swapFake) Quote(context.Context, swaps.QuoteInput) (swaps.Quote, error) {
	p.calls++
	return swaps.Quote{}, p.err
}
func (p *swapFake) Status(context.Context, swaps.StatusInput) (swaps.Status, error) {
	p.calls++
	return swaps.Status{State: "unknown"}, p.err
}
func TestSwapRouteDisabledAndNoFallback(t *testing.T) {
	p := &swapFake{}
	legacy := &providerFake{id: "legacy"}
	r := New([]data.Provider{p, legacy}, settingsFake{{"lifi", false, 1}, {"legacy", true, 2}})
	if _, _, e := r.SwapTokens(context.Background(), swaps.TokensInput{Network: "eip155:1"}); e != data.ErrUnavailable || p.calls != 0 {
		t.Fatal(e)
	}
	r = New([]data.Provider{p, legacy}, settingsFake{{"lifi", true, 1}, {"legacy", true, 2}})
	p.err = data.ErrRateLimited
	if _, _, e := r.SwapTokens(context.Background(), swaps.TokensInput{Network: "eip155:1"}); e != data.ErrRateLimited || p.calls != 1 || legacy.calls != 0 {
		t.Fatal(e)
	}
}
