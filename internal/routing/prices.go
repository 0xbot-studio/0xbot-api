package routing

import (
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/prices"
	"context"
	"time"
)

func (r *Router) AssetPrice(ctx context.Context, in prices.AssetRequest) (prices.Quote, string, error) {
	if err := in.Validate(); err != nil {
		return prices.Quote{}, "", err
	}
	return routeValidated(r, ctx, data.Request{Network: "market", Provider: in.Provider}, "market.asset_price", 7*time.Second, func(ctx context.Context, p data.Provider) (prices.Quote, error) {
		v, ok := p.(prices.Provider)
		if !ok {
			return prices.Quote{}, data.ErrUnavailable
		}
		return v.AssetPrice(ctx, in)
	})
}
func (r *Router) TokenPrice(ctx context.Context, in prices.TokenRequest) (prices.Quote, string, error) {
	if err := in.Validate(); err != nil {
		return prices.Quote{}, "", err
	}
	return routeValidated(r, ctx, data.Request{Network: in.Network, Provider: in.Provider}, "market.token_price", 7*time.Second, func(ctx context.Context, p data.Provider) (prices.Quote, error) {
		v, ok := p.(prices.Provider)
		if !ok {
			return prices.Quote{}, data.ErrUnavailable
		}
		return v.TokenPrice(ctx, in)
	})
}
