package operations

import (
	"0xbot.org/api/internal/capabilities"
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/nft"
	"context"
	"time"
)

type nftRouter interface {
	NFTDetail(context.Context, nft.TokenRequest) (nft.Detail, string, error)
	NFTOwner(context.Context, nft.TokenRequest) (nft.Owner, string, error)
	NFTBalance(context.Context, nft.BalanceRequest) (nft.Balance, string, error)
}

func (s *Service) NFTDetail(ctx context.Context, workspace string, in nft.TokenRequest) (nft.DetailResult, error) {
	if err := in.Validate(); err != nil {
		return nft.DetailResult{}, err
	}
	return nftQuery(s, ctx, workspace, capabilities.NFTDetail, func(ctx context.Context, r nftRouter) (nft.Detail, string, error) { return r.NFTDetail(ctx, in) })
}
func (s *Service) NFTOwner(ctx context.Context, workspace string, in nft.TokenRequest) (nft.OwnerResult, error) {
	if err := in.Validate(); err != nil {
		return nft.OwnerResult{}, err
	}
	return nftQuery(s, ctx, workspace, capabilities.NFTOwner, func(ctx context.Context, r nftRouter) (nft.Owner, string, error) { return r.NFTOwner(ctx, in) })
}
func (s *Service) NFTBalance(ctx context.Context, workspace string, in nft.BalanceRequest) (nft.BalanceResult, error) {
	if err := in.Validate(); err != nil {
		return nft.BalanceResult{}, err
	}
	return nftQuery(s, ctx, workspace, capabilities.NFTBalance, func(ctx context.Context, r nftRouter) (nft.Balance, string, error) { return r.NFTBalance(ctx, in) })
}
func nftQuery[T any](s *Service, ctx context.Context, workspace, capability string, invoke func(context.Context, nftRouter) (T, string, error)) (data.QueryResult[T], error) {
	if workspace == "" {
		return data.QueryResult[T]{}, data.ErrInput
	}
	if s.limiter != nil {
		ok, err := s.limiter.Allow(ctx, "nft-query:"+workspace, 20, time.Minute)
		if err != nil {
			return data.QueryResult[T]{}, err
		}
		if !ok {
			return data.QueryResult[T]{}, data.ErrRateLimited
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	payload, provider, record, err := execute(s, ctx, workspace, capability, func() (T, string, error) {
		r, ok := s.router.(nftRouter)
		if !ok {
			var zero T
			return zero, "", data.ErrUnavailable
		}
		return invoke(ctx, r)
	})
	if err != nil {
		return data.QueryResult[T]{}, err
	}
	return data.QueryResult[T]{ID: record.ID, Provider: provider, Status: record.Status, LatencyMS: record.LatencyMS, FeeUSD: "0", Data: payload}, nil
}
