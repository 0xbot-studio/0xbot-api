package operations

import (
	"0xbot.org/api/internal/capabilities"
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/tokenbound"
	"context"
	"time"
)

type tokenBoundRouter interface {
	ResolveTokenBoundAccount(context.Context, tokenbound.ResolveRequest) (tokenbound.Account, string, error)
	GetTokenBoundAccount(context.Context, tokenbound.GetRequest) (tokenbound.Account, string, error)
}

func (s *Service) ResolveTokenBoundAccount(ctx context.Context, workspace string, in tokenbound.ResolveRequest) (tokenbound.Result, error) {
	if err := in.Validate(); err != nil {
		return tokenbound.Result{}, err
	}
	return s.tokenBoundQuery(ctx, workspace, capabilities.ResolveTokenBoundAccount, func(ctx context.Context, r tokenBoundRouter) (tokenbound.Account, string, error) {
		return r.ResolveTokenBoundAccount(ctx, in)
	})
}
func (s *Service) GetTokenBoundAccount(ctx context.Context, workspace string, in tokenbound.GetRequest) (tokenbound.Result, error) {
	if err := in.Validate(); err != nil {
		return tokenbound.Result{}, err
	}
	return s.tokenBoundQuery(ctx, workspace, capabilities.GetTokenBoundAccount, func(ctx context.Context, r tokenBoundRouter) (tokenbound.Account, string, error) {
		return r.GetTokenBoundAccount(ctx, in)
	})
}
func (s *Service) tokenBoundQuery(ctx context.Context, workspace, capability string, invoke func(context.Context, tokenBoundRouter) (tokenbound.Account, string, error)) (tokenbound.Result, error) {
	if workspace == "" {
		return tokenbound.Result{}, data.ErrInput
	}
	if s.limiter != nil {
		ok, err := s.limiter.Allow(ctx, "token-bound-query:"+workspace, 20, time.Minute)
		if err != nil {
			return tokenbound.Result{}, err
		}
		if !ok {
			return tokenbound.Result{}, data.ErrRateLimited
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	payload, provider, record, err := execute(s, ctx, workspace, capability, func() (tokenbound.Account, string, error) {
		r, ok := s.router.(tokenBoundRouter)
		if !ok {
			return tokenbound.Account{}, "", data.ErrUnavailable
		}
		return invoke(ctx, r)
	})
	if err != nil {
		return tokenbound.Result{}, err
	}
	return tokenbound.Result{ID: record.ID, Provider: provider, Status: record.Status, LatencyMS: record.LatencyMS, FeeUSD: "0", Data: payload}, nil
}
