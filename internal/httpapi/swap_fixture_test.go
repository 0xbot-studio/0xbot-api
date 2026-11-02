package httpapi

import (
	"0xbot.org/api/internal/swaps"
	"context"
	"time"
)

type swapFixture struct{}

func (*swapFixture) ID() string             { return "lifi" }
func (*swapFixture) Supports(n string) bool { return swaps.Chain(n) != "" }
func (*swapFixture) CapabilityIDs() []string {
	return []string{"swaps.tokens", "swaps.quote", "swaps.status"}
}
func (*swapFixture) Tokens(_ context.Context, in swaps.TokensInput) ([]swaps.Token, error) {
	return []swaps.Token{{Network: in.Network, Address: swaps.Native, Symbol: "ETH", Name: "Ether", Decimals: 18}}, nil
}
func (*swapFixture) Quote(_ context.Context, in swaps.QuoteInput) (swaps.Quote, error) {
	return swaps.Quote{ID: "fixture", Request: in, ToAmount: "123456789012345678901", MinimumAmount: "122456789012345678901", FetchedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Minute), Costs: []swaps.Cost{}}, nil
}
func (*swapFixture) Status(_ context.Context, in swaps.StatusInput) (swaps.Status, error) {
	return swaps.Status{TxHash: in.TxHash, State: "unknown"}, nil
}
