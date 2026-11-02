package routing

import (
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/swaps"
	"context"
	"time"
)

func (r *Router) SwapTokens(ctx context.Context, in swaps.TokensInput) ([]swaps.Token, string, error) {
	var zero []swaps.Token
	if e := in.Validate(); e != nil {
		return zero, "", e
	}
	return routeValidated(r, ctx, data.Request{Network: in.Network, Provider: "lifi"}, "swaps.tokens", 20*time.Second, func(ctx context.Context, p data.Provider) ([]swaps.Token, error) {
		v, ok := p.(swaps.Provider)
		if !ok {
			return zero, data.ErrUnavailable
		}
		return v.Tokens(ctx, in)
	})
}
func (r *Router) SwapQuote(ctx context.Context, in swaps.QuoteInput) (swaps.Quote, string, error) {
	var zero swaps.Quote
	if e := in.Validate(); e != nil {
		return zero, "", e
	}
	return routeValidated(r, ctx, data.Request{Network: in.FromNetwork, Provider: "lifi"}, "swaps.quote", 20*time.Second, func(ctx context.Context, p data.Provider) (swaps.Quote, error) {
		v, ok := p.(swaps.Provider)
		if !ok {
			return zero, data.ErrUnavailable
		}
		return v.Quote(ctx, in)
	})
}
func (r *Router) SwapStatus(ctx context.Context, in swaps.StatusInput) (swaps.Status, string, error) {
	var zero swaps.Status
	if e := in.Validate(); e != nil {
		return zero, "", e
	}
	return routeValidated(r, ctx, data.Request{Network: in.FromNetwork, Provider: "lifi"}, "swaps.status", 20*time.Second, func(ctx context.Context, p data.Provider) (swaps.Status, error) {
		v, ok := p.(swaps.Provider)
		if !ok {
			return zero, data.ErrUnavailable
		}
		return v.Status(ctx, in)
	})
}
