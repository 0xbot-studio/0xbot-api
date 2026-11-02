package routing

import (
	"0xbot.org/api/internal/capabilities"
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/tokenbound"
	"context"
	"time"
)

func (r *Router) ResolveTokenBoundAccount(ctx context.Context, in tokenbound.ResolveRequest) (tokenbound.Account, string, error) {
	if err := in.Validate(); err != nil {
		return tokenbound.Account{}, "", err
	}
	return route(r, ctx, data.Request{Address: in.TokenContract, Network: in.Network, Provider: in.Provider}, capabilities.ResolveTokenBoundAccount, 8*time.Second, func(ctx context.Context, p data.Provider) (tokenbound.Account, error) {
		adapter, ok := p.(tokenbound.Provider)
		if !ok {
			return tokenbound.Account{}, data.ErrUnavailable
		}
		return adapter.ResolveTokenBoundAccount(ctx, in)
	})
}
func (r *Router) GetTokenBoundAccount(ctx context.Context, in tokenbound.GetRequest) (tokenbound.Account, string, error) {
	if err := in.Validate(); err != nil {
		return tokenbound.Account{}, "", err
	}
	return route(r, ctx, data.Request{Address: in.Address, Network: in.Network, Provider: in.Provider}, capabilities.GetTokenBoundAccount, 8*time.Second, func(ctx context.Context, p data.Provider) (tokenbound.Account, error) {
		adapter, ok := p.(tokenbound.Provider)
		if !ok {
			return tokenbound.Account{}, data.ErrUnavailable
		}
		return adapter.GetTokenBoundAccount(ctx, in)
	})
}
