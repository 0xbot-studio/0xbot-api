package operations

import (
	"0xbot.org/api/internal/capabilities"
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/prices"
	"context"
	"time"
)

type rwaRouter interface {
	RWATokenBalance(context.Context, data.EquityTokenRequest) (data.Balance, string, error)
	EquityPrice(context.Context, prices.EquityRequest) (prices.Quote, string, error)
}

func (s *Service) RWATokenBalance(ctx context.Context, workspace string, in data.EquityTokenRequest) (data.Result, error) {
	if _, e := in.Request(); e != nil {
		return data.Result{}, e
	}
	if workspace == "" {
		return data.Result{}, data.ErrInput
	}
	if s.limiter != nil {
		ok, err := s.limiter.Allow(ctx, "rwa-query:"+workspace, 20, time.Minute)
		if err != nil {
			return data.Result{}, err
		}
		if !ok {
			return data.Result{}, data.ErrRateLimited
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	items, p, record, e := execute(s, ctx, workspace, capabilities.RWATokenBalance, func() ([]data.Balance, string, error) {
		r, ok := s.router.(rwaRouter)
		if !ok {
			return nil, "", data.ErrUnavailable
		}
		b, p, e := r.RWATokenBalance(ctx, in)
		return []data.Balance{b}, p, e
	})
	if e != nil {
		return data.Result{}, e
	}
	return data.Result{ID: record.ID, Provider: p, Status: record.Status, LatencyMS: record.LatencyMS, FeeUSD: "0", Data: items[0]}, nil
}

func (s *Service) EquityPrice(ctx context.Context, workspace string, in prices.EquityRequest) (prices.Result, error) {
	if err := in.Validate(); err != nil {
		return prices.Result{}, err
	}
	if workspace == "" {
		return prices.Result{}, data.ErrInput
	}
	// Equity prices share the existing price-query budget with asset and token prices.
	if s.limiter != nil {
		ok, err := s.limiter.Allow(ctx, "price-query:"+workspace, 20, time.Minute)
		if err != nil {
			return prices.Result{}, err
		}
		if !ok {
			return prices.Result{}, data.ErrRateLimited
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	payload, provider, record, err := execute(s, ctx, workspace, capabilities.EquityPrice, func() (prices.Quote, string, error) {
		r, ok := s.router.(rwaRouter)
		if !ok {
			return prices.Quote{}, "", data.ErrUnavailable
		}
		return r.EquityPrice(ctx, in)
	})
	if err != nil {
		return prices.Result{}, err
	}
	return prices.Result{ID: record.ID, Provider: provider, Status: record.Status, LatencyMS: record.LatencyMS, FeeUSD: "0", Data: payload}, nil
}
