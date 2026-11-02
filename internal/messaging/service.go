package messaging

import (
	"0xbot.org/api/internal/access"
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/operations"
	"0xbot.org/api/internal/platform/token"
	"context"
	"time"
)

type Identity interface {
	OwnsWallet(context.Context, string, string) (bool, error)
}

func (s *Service) recordRead(ctx context.Context, capability string, start time.Time, err error) (string, error) {
	r := operations.Record{ID: token.New(), WorkspaceID: access.From(ctx).Workspace, Capability: capability, Provider: ProviderID, Status: "succeeded", FeeUSD: "0", CreatedAt: start, LatencyMS: time.Since(start).Milliseconds()}
	if err != nil {
		r.Status = "failed"
		r.ErrorCode = "message_query_failed"
	}
	if s.Recorder != nil {
		return r.ID, s.Recorder.Record(ctx, r)
	}
	return r.ID, nil
}

type Limiter interface {
	Allow(context.Context, string, int, time.Duration) (bool, error)
}
type Availability interface {
	Enabled(context.Context, string) (bool, error)
}
type Service struct {
	Provider     Provider
	Identity     Identity
	Limiter      Limiter
	Availability Availability
	Recorder     operations.Recorder
}

func (s *Service) authorize(ctx context.Context, wallet, scope string) error {
	p := access.From(ctx)
	if p.Workspace == "" || !p.Has(scope) || (p.Wallet != "" && p.Wallet != wallet) {
		return ErrPermission
	}
	owns, e := s.Identity.OwnsWallet(ctx, p.Workspace, wallet)
	if e != nil {
		return e
	}
	if !owns {
		return ErrPermission
	}
	enabled, e := s.Availability.Enabled(ctx, ProviderID)
	if e != nil {
		return e
	}
	if !enabled {
		return data.ErrUnavailable
	}
	ok, e := s.Limiter.Allow(ctx, "messages:"+p.Workspace, 30, time.Minute)
	if e != nil {
		return e
	}
	if !ok {
		return data.ErrRateLimited
	}
	return nil
}
func (s *Service) Send(ctx context.Context, in SendInput) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if e := in.Validate(); e != nil {
		return Result{}, e
	}
	if e := s.authorize(ctx, in.From, "messages:send"); e != nil {
		return Result{}, e
	}
	start := time.Now()
	out, e := s.Provider.Send(ctx, access.From(ctx).Workspace, in)
	if e != nil {
		return Result{}, e
	}
	return Result{ID: out.ID, Provider: s.Provider.ID(), Status: "succeeded", LatencyMS: time.Since(start).Milliseconds(), FeeUSD: "0", Data: out}, nil
}
func (s *Service) List(ctx context.Context, in ListInput) (ListResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if e := in.Validate(); e != nil {
		return ListResult{}, e
	}
	if e := s.authorize(ctx, in.Address, "messages:read"); e != nil {
		return ListResult{}, e
	}
	start := time.Now()
	out, e := s.Provider.List(ctx, in)
	id, recordErr := s.recordRead(ctx, "messaging.list", start, e)
	if recordErr != nil {
		return ListResult{}, recordErr
	}
	if e != nil {
		return ListResult{}, e
	}
	return ListResult{ID: id, Provider: s.Provider.ID(), Status: "succeeded", LatencyMS: time.Since(start).Milliseconds(), FeeUSD: "0", Data: out}, nil
}
func (s *Service) Get(ctx context.Context, in GetInput) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if e := in.Validate(); e != nil {
		return Result{}, e
	}
	if e := s.authorize(ctx, in.Address, "messages:read"); e != nil {
		return Result{}, e
	}
	start := time.Now()
	out, e := s.Provider.Get(ctx, in)
	id, recordErr := s.recordRead(ctx, "messaging.get", start, e)
	if recordErr != nil {
		return Result{}, recordErr
	}
	if e != nil {
		return Result{}, e
	}
	return Result{ID: id, Provider: s.Provider.ID(), Status: "succeeded", LatencyMS: time.Since(start).Milliseconds(), FeeUSD: "0", Data: out}, nil
}
