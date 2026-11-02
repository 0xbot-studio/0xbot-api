package routing

import (
	"0xbot.org/api/internal/capabilities"
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/prices"
	"context"
	"time"
)

func (r *Router) RWATokenBalance(ctx context.Context, in data.EquityTokenRequest) (data.Balance, string, error) {
	req, err := in.Request()
	if err != nil {
		return data.Balance{}, "", err
	}
	return route(r, ctx, req, capabilities.RWATokenBalance, r.timeout, func(ctx context.Context, p data.Provider) (data.Balance, error) {
		reader, ok := p.(interface {
			RWATokenBalance(context.Context, data.Request) (data.Balance, error)
		})
		if !ok {
			return data.Balance{}, data.ErrUnavailable
		}
		return reader.RWATokenBalance(ctx, req)
	})
}

func (r *Router) EquityPrice(ctx context.Context, in prices.EquityRequest) (prices.Quote, string, error) {
	if err := in.Validate(); err != nil {
		return prices.Quote{}, "", err
	}
	return routeValidated(r, ctx, data.Request{Network: in.Network, Provider: in.Provider}, capabilities.EquityPrice, 7*time.Second, func(ctx context.Context, p data.Provider) (prices.Quote, error) {
		reader, ok := p.(interface {
			EquityPrice(context.Context, prices.EquityRequest) (prices.Quote, error)
		})
		if !ok {
			return prices.Quote{}, data.ErrUnavailable
		}
		return reader.EquityPrice(ctx, in)
	})
}
