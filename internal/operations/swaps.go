package operations

import (
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/swaps"
	"context"
	"time"
)

func (s *Service) SwapTokens(ctx context.Context, w string, in swaps.TokensInput) (swaps.TokensResult, error) {
	if e := in.Validate(); e != nil {
		return swaps.TokensResult{}, e
	}
	if w == "" {
		return swaps.TokensResult{}, data.ErrInput
	}
	if s.limiter != nil {
		ok, e := s.limiter.Allow(ctx, "swap-query:"+w, 30, time.Minute)
		if e != nil {
			return swaps.TokensResult{}, e
		}
		if !ok {
			return swaps.TokensResult{}, data.ErrRateLimited
		}
	}
	out, p, record, e := execute(s, ctx, w, "swaps.tokens", func() ([]swaps.Token, string, error) {
		r, ok := s.router.(interface {
			SwapTokens(context.Context, swaps.TokensInput) ([]swaps.Token, string, error)
		})
		if !ok {
			var zero []swaps.Token
			return zero, "", data.ErrUnavailable
		}
		return r.SwapTokens(ctx, in)
	})
	if e != nil {
		return swaps.TokensResult{}, e
	}
	return swaps.TokensResult{ID: record.ID, Provider: p, Status: record.Status, LatencyMS: record.LatencyMS, FeeUSD: "0", Data: out}, nil
}
func (s *Service) SwapQuote(ctx context.Context, w string, in swaps.QuoteInput) (swaps.QuoteResult, error) {
	if e := in.Validate(); e != nil {
		return swaps.QuoteResult{}, e
	}
	if w == "" {
		return swaps.QuoteResult{}, data.ErrInput
	}
	if s.limiter != nil {
		ok, e := s.limiter.Allow(ctx, "swap-query:"+w, 30, time.Minute)
		if e != nil {
			return swaps.QuoteResult{}, e
		}
		if !ok {
			return swaps.QuoteResult{}, data.ErrRateLimited
		}
	}
	out, p, record, e := execute(s, ctx, w, "swaps.quote", func() (swaps.Quote, string, error) {
		r, ok := s.router.(interface {
			SwapQuote(context.Context, swaps.QuoteInput) (swaps.Quote, string, error)
		})
		if !ok {
			var zero swaps.Quote
			return zero, "", data.ErrUnavailable
		}
		return r.SwapQuote(ctx, in)
	})
	if e != nil {
		return swaps.QuoteResult{}, e
	}
	return swaps.QuoteResult{ID: record.ID, Provider: p, Status: record.Status, LatencyMS: record.LatencyMS, FeeUSD: "0", Data: out}, nil
}
func (s *Service) SwapStatus(ctx context.Context, w string, in swaps.StatusInput) (swaps.StatusResult, error) {
	if e := in.Validate(); e != nil {
		return swaps.StatusResult{}, e
	}
	if w == "" {
		return swaps.StatusResult{}, data.ErrInput
	}
	if s.limiter != nil {
		ok, e := s.limiter.Allow(ctx, "swap-query:"+w, 30, time.Minute)
		if e != nil {
			return swaps.StatusResult{}, e
		}
		if !ok {
			return swaps.StatusResult{}, data.ErrRateLimited
		}
	}
	out, p, record, e := execute(s, ctx, w, "swaps.status", func() (swaps.Status, string, error) {
		r, ok := s.router.(interface {
			SwapStatus(context.Context, swaps.StatusInput) (swaps.Status, string, error)
		})
		if !ok {
			var zero swaps.Status
			return zero, "", data.ErrUnavailable
		}
		return r.SwapStatus(ctx, in)
	})
	if e != nil {
		return swaps.StatusResult{}, e
	}
	return swaps.StatusResult{ID: record.ID, Provider: p, Status: record.Status, LatencyMS: record.LatencyMS, FeeUSD: "0", Data: out}, nil
}
