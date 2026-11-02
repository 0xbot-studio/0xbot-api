package routing

import (
	"0xbot.org/api/internal/capabilities"
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/nft"
	"context"
	"time"
)

func (r *Router) NFTDetail(ctx context.Context, in nft.TokenRequest) (nft.Detail, string, error) {
	if err := in.Validate(); err != nil {
		return nft.Detail{}, "", err
	}
	return route(r, ctx, data.Request{Address: in.Contract, Network: in.Network, Provider: in.Provider}, capabilities.NFTDetail, 12*time.Second, func(ctx context.Context, p data.Provider) (nft.Detail, error) {
		adapter, ok := p.(nft.Provider)
		if !ok {
			return nft.Detail{}, data.ErrUnavailable
		}
		return adapter.NFTDetail(ctx, in)
	})
}
func (r *Router) NFTOwner(ctx context.Context, in nft.TokenRequest) (nft.Owner, string, error) {
	if err := in.Validate(); err != nil {
		return nft.Owner{}, "", err
	}
	return route(r, ctx, data.Request{Address: in.Contract, Network: in.Network, Provider: in.Provider}, capabilities.NFTOwner, 12*time.Second, func(ctx context.Context, p data.Provider) (nft.Owner, error) {
		adapter, ok := p.(nft.Provider)
		if !ok {
			return nft.Owner{}, data.ErrUnavailable
		}
		return adapter.NFTOwner(ctx, in)
	})
}
func (r *Router) NFTBalance(ctx context.Context, in nft.BalanceRequest) (nft.Balance, string, error) {
	if err := in.Validate(); err != nil {
		return nft.Balance{}, "", err
	}
	return route(r, ctx, data.Request{Address: in.Contract, Network: in.Network, Provider: in.Provider}, capabilities.NFTBalance, 12*time.Second, func(ctx context.Context, p data.Provider) (nft.Balance, error) {
		adapter, ok := p.(nft.Provider)
		if !ok {
			return nft.Balance{}, data.ErrUnavailable
		}
		return adapter.NFTBalance(ctx, in)
	})
}
