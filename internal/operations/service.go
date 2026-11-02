package operations

import (
	"0xbot.org/api/internal/capabilities"
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/nft"
	"0xbot.org/api/internal/platform/token"
	"0xbot.org/api/internal/prices"
	"0xbot.org/api/internal/tokenbound"
	"context"
	"errors"
	"time"
)

type Router interface {
	Balance(context.Context, data.Request) (data.Balance, string, error)
	Balances(context.Context, data.Request) ([]data.Balance, string, error)
}
type Limiter interface {
	Allow(context.Context, string, int, time.Duration) (bool, error)
}
type Recorder interface {
	Record(context.Context, Record) error
}
type Record struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"-"`
	Capability  string    `json:"capability"`
	Provider    string    `json:"provider"`
	Status      string    `json:"status"`
	LatencyMS   int64     `json:"latency_ms"`
	ErrorCode   string    `json:"error_code"`
	FeeUSD      string    `json:"fee_usd"`
	CreatedAt   time.Time `json:"created_at"`
}
type Service struct {
	router   Router
	recorder Recorder
	limiter  Limiter
}

func New(r Router, s Recorder, limits ...Limiter) *Service {
	service := &Service{router: r, recorder: s}
	if len(limits) > 0 {
		service.limiter = limits[0]
	}
	return service
}
func (s *Service) Balance(ctx context.Context, workspace string, req data.Request) (data.Result, error) {
	if e := req.Validate(); e != nil {
		return data.Result{}, e
	}
	items, p, record, e := execute(s, ctx, workspace, capabilities.NativeBalance, func() ([]data.Balance, string, error) {
		b, p, e := s.router.Balance(ctx, req)
		return []data.Balance{b}, p, e
	})
	if e != nil {
		return data.Result{}, e
	}
	return data.Result{ID: record.ID, Provider: p, Status: record.Status, LatencyMS: record.LatencyMS, FeeUSD: "0", Data: items[0]}, nil
}

func (s *Service) TokenBalance(ctx context.Context, workspace string, in data.TokenRequest) (data.Result, error) {
	req, e := in.Request()
	if e != nil {
		return data.Result{}, e
	}
	items, p, record, e := execute(s, ctx, workspace, capabilities.TokenBalance, func() ([]data.Balance, string, error) {
		b, p, e := s.router.Balance(ctx, req)
		return []data.Balance{b}, p, e
	})
	if e != nil {
		return data.Result{}, e
	}
	return data.Result{ID: record.ID, Provider: p, Status: record.Status, LatencyMS: record.LatencyMS, FeeUSD: "0", Data: items[0]}, nil
}

func (s *Service) Balances(ctx context.Context, workspace string, req data.Request) (data.BalancesResult, error) {
	if e := req.Validate(); e != nil || req.Contract != "" {
		return data.BalancesResult{}, data.ErrInput
	}
	if s.limiter != nil {
		ok, e := s.limiter.Allow(ctx, "batch-query:"+workspace, 20, time.Minute)
		if e != nil {
			return data.BalancesResult{}, e
		}
		if !ok {
			return data.BalancesResult{}, data.ErrRateLimited
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	items, p, record, e := execute(s, ctx, workspace, capabilities.CommonBalances, func() ([]data.Balance, string, error) { return s.router.Balances(ctx, req) })
	if e != nil {
		return data.BalancesResult{}, e
	}
	return data.BalancesResult{ID: record.ID, Provider: p, Status: record.Status, LatencyMS: record.LatencyMS, FeeUSD: "0", Data: items}, nil
}

func execute[T any](s *Service, ctx context.Context, workspace, capability string, invoke func() (T, string, error)) (T, string, Record, error) {
	id := token.New()
	start := time.Now()
	b, p, e := invoke()
	status, code := "succeeded", ""
	if e != nil {
		status = "failed"
		code = "provider_unavailable"
		for _, kind := range []error{prices.ErrNotFound, prices.ErrStale, prices.ErrConfidence, data.ErrRateLimited} {
			if errors.Is(e, kind) {
				code = kind.Error()
			}
		}
		if errors.Is(e, data.ErrProvider) {
			code = "unsupported_provider"
		}
		if errors.Is(e, tokenbound.ErrNotAccount) {
			code = "not_token_bound_account"
		}
		if errors.Is(e, nft.ErrUnsupported) {
			code = "unsupported_nft_standard"
		}
		if errors.Is(e, nft.ErrReverted) {
			code = "nft_query_reverted"
		}
	}
	record := Record{ID: id, WorkspaceID: workspace, Capability: capability, Provider: p, Status: status, LatencyMS: time.Since(start).Milliseconds(), ErrorCode: code, FeeUSD: "0", CreatedAt: time.Now().UTC()}
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if pe := s.recorder.Record(persistCtx, record); pe != nil {
		var zero T
		return zero, p, record, pe
	}
	return b, p, record, e
}
