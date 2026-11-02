package operations

import (
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/prices"
	"context"
	"time"
)

type priceRouter interface {
	AssetPrice(context.Context, prices.AssetRequest) (prices.Quote, string, error)
	TokenPrice(context.Context, prices.TokenRequest) (prices.Quote, string, error)
}

func (s *Service) AssetPrice(ctx context.Context, w string, in prices.AssetRequest) (prices.Result, error) {
	if err := in.Validate(); err != nil {
		return prices.Result{}, err
	}
	return s.priceQuery(ctx, w, "market.asset_price", func(r priceRouter) (prices.Quote, string, error) { return r.AssetPrice(ctx, in) })
}
func (s *Service) TokenPrice(ctx context.Context, w string, in prices.TokenRequest) (prices.Result, error) {
	if err := in.Validate(); err != nil {
		return prices.Result{}, err
	}
	return s.priceQuery(ctx, w, "market.token_price", func(r priceRouter) (prices.Quote, string, error) { return r.TokenPrice(ctx, in) })
}
func (s *Service) priceQuery(ctx context.Context, w, cap string, call func(priceRouter) (prices.Quote, string, error)) (prices.Result, error) {
	if w == "" {
		return prices.Result{}, data.ErrInput
	}
	if s.limiter != nil {
		ok, err := s.limiter.Allow(ctx, "price-query:"+w, 20, time.Minute)
		if err != nil {
			return prices.Result{}, err
		}
		if !ok {
			return prices.Result{}, data.ErrRateLimited
		}
	}
	q, p, record, err := execute(s, ctx, w, cap, func() (prices.Quote, string, error) {
		r, ok := s.router.(priceRouter)
		if !ok {
			return prices.Quote{}, "", data.ErrUnavailable
		}
		return call(r)
	})
	if err != nil {
		return prices.Result{}, err
	}
	return prices.Result{ID: record.ID, Provider: p, Status: record.Status, LatencyMS: record.LatencyMS, FeeUSD: "0", Data: q}, nil
}
