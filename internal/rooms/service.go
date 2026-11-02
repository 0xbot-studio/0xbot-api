package rooms

import (
	"0xbot.org/api/internal/access"
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/operations"
	"0xbot.org/api/internal/platform/token"
	"context"
	"sync"
	"time"
)

type Provider interface {
	Mutate(context.Context, string, string, Command) (Result, error)
	Get(context.Context, string) (Room, error)
	List(context.Context, string, int) (Listing, error)
}
type Limiter interface {
	Allow(context.Context, string, int, time.Duration) (bool, error)
}
type Availability interface {
	Enabled(context.Context, string) (bool, error)
}
type Service struct {
	Provider     Provider
	Limiter      Limiter
	Availability Availability
	Recorder     operations.Recorder
	mu           sync.Mutex
	waiting      map[string]int
	total        int
}

func Actor(workspace string) string { return token.Hash("padin-rooms:participant:" + workspace) }
func (s *Service) authorize(ctx context.Context, scope string) error {
	p := access.From(ctx)
	if p.Workspace == "" || !p.Has(scope) {
		return ErrPermission
	}
	if s.Availability != nil {
		ok, e := s.Availability.Enabled(ctx, ProviderID)
		if e != nil {
			return e
		}
		if !ok {
			return data.ErrUnavailable
		}
	}
	if s.Limiter != nil {
		ok, e := s.Limiter.Allow(ctx, "rooms:"+p.Workspace, 40, time.Minute)
		if e != nil {
			return e
		}
		if !ok {
			return data.ErrRateLimited
		}
	}
	return nil
}
func (s *Service) mutate(ctx context.Context, c Command) (Result, error) {
	if c.Action == "create" || c.Action == "summary" || c.Action == "promote" || c.Action == "demote" || c.Action == "start" || c.Action == "skip" || c.Action == "close" {
		c.AdminUser = administrator(ctx)
		if c.AdminUser == "" {
			return Result{}, ErrPermission
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if e := c.Validate(); e != nil {
		return Result{}, e
	}
	if e := s.authorize(ctx, "rooms:write"); e != nil {
		return Result{}, e
	}
	p := access.From(ctx)
	return s.Provider.Mutate(ctx, p.Workspace, Actor(p.Workspace), c)
}
func (s *Service) Create(ctx context.Context, in CreateInput) (Result, error) {
	return s.mutate(ctx, Command{Action: "create", Topic: in.Topic, Name: in.Name, MaxRounds: in.MaxRounds, IdempotencyKey: in.IdempotencyKey})
}
func (s *Service) Join(ctx context.Context, in JoinInput) (Result, error) {
	return s.mutate(ctx, Command{Action: "join", RoomID: in.RoomID, Name: in.Name, IdempotencyKey: in.IdempotencyKey})
}
func (s *Service) Moderate(ctx context.Context, in ModerateInput) (Result, error) {
	switch in.Action {
	case "promote", "demote", "start", "skip", "close":
	default:
		return Result{}, data.ErrInput
	}
	return s.mutate(ctx, Command{Action: in.Action, RoomID: in.RoomID, ParticipantID: in.ParticipantID, ExpectedSeq: in.ExpectedSeq, IdempotencyKey: in.IdempotencyKey})
}
func (s *Service) Post(ctx context.Context, in PostInput) (Result, error) {
	return s.mutate(ctx, Command{Action: "post", RoomID: in.RoomID, Body: in.Body, ExpectedTurn: in.ExpectedTurn, ReplyTo: in.ReplyTo, IdempotencyKey: in.IdempotencyKey})
}
func (s *Service) Question(ctx context.Context, in QuestionInput) (Result, error) {
	return s.mutate(ctx, Command{Action: "question", RoomID: in.RoomID, Body: in.Body, IdempotencyKey: in.IdempotencyKey})
}
func (s *Service) SubmitSummary(ctx context.Context, in SummaryInput) (Result, error) {
	return s.mutate(ctx, Command{Action: "summary", RoomID: in.RoomID, Body: in.Body, ExpectedSeq: in.ThroughSeq, MessageSeqs: in.MessageSeqs, IdempotencyKey: in.IdempotencyKey})
}
func (s *Service) Get(ctx context.Context, in RoomInput) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if !IDPattern.MatchString(in.RoomID) {
		return Result{}, data.ErrInput
	}
	if e := s.authorize(ctx, "rooms:read"); e != nil {
		return Result{}, e
	}
	r, e := s.Provider.Get(ctx, in.RoomID)
	if e != nil {
		return Result{}, e
	}
	return readResult(ctx, s, "rooms.get", Snapshot{Room: r, SelfID: Actor(access.From(ctx).Workspace)})
}
func (s *Service) List(ctx context.Context, in ListInput) (ListResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if in.Limit == 0 {
		in.Limit = 20
	}
	if in.Limit < 1 || in.Limit > 50 || (in.BeforeID != "" && !IDPattern.MatchString(in.BeforeID)) {
		return ListResult{}, data.ErrInput
	}
	if e := s.authorize(ctx, "rooms:read"); e != nil {
		return ListResult{}, e
	}
	r, e := s.Provider.List(ctx, in.BeforeID, in.Limit)
	if e != nil {
		return ListResult{}, e
	}
	return readResult(ctx, s, "rooms.list", r)
}
func (s *Service) Events(ctx context.Context, in EventsInput) (EventsResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if in.Limit == 0 {
		in.Limit = 50
	}
	if !IDPattern.MatchString(in.RoomID) || in.AfterSeq < 0 || in.Limit < 1 || in.Limit > 100 {
		return EventsResult{}, data.ErrInput
	}
	if e := s.authorize(ctx, "rooms:read"); e != nil {
		return EventsResult{}, e
	}
	out, e := s.events(ctx, in)
	if e != nil {
		return out, e
	}
	return readResult(ctx, s, "rooms.events", out.Data)
}
func (s *Service) events(ctx context.Context, in EventsInput) (EventsResult, error) {
	r, e := s.Provider.Get(ctx, in.RoomID)
	if e != nil {
		return EventsResult{}, e
	}
	if in.AfterSeq > r.Seq {
		return EventsResult{}, data.ErrInput
	}
	return EventsResult{ID: token.New(), Provider: ProviderID, Status: "succeeded", FeeUSD: "0", Data: r.Page(in.AfterSeq, in.Limit)}, nil
}

// Wait is bounded long polling, not a promise of model wake-up or a durable subscription.
func (s *Service) Wait(ctx context.Context, in WaitInput) (EventsResult, error) {
	if in.TimeoutSeconds == 0 {
		in.TimeoutSeconds = 20
	}
	if in.TimeoutSeconds < 1 || in.TimeoutSeconds > 20 || !IDPattern.MatchString(in.RoomID) || in.AfterSeq < 0 {
		return EventsResult{}, data.ErrInput
	}
	if e := s.authorize(ctx, "rooms:read"); e != nil {
		return EventsResult{}, e
	}
	release, e := s.acquireWait(ctx)
	if e != nil {
		return EventsResult{}, e
	}
	defer release()
	return s.wait(ctx, in)
}

// Shared by bounded MCP waits and browser streams.
func (s *Service) acquireWait(ctx context.Context) (func(), error) {
	w := access.From(ctx).Workspace
	s.mu.Lock()
	if s.waiting == nil {
		s.waiting = map[string]int{}
	}
	if s.total >= 128 || s.waiting[w] >= 2 {
		s.mu.Unlock()
		return nil, data.ErrRateLimited
	}
	s.waiting[w]++
	s.total++
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		s.waiting[w]--
		if s.waiting[w] == 0 {
			delete(s.waiting, w)
		}
		s.total--
		s.mu.Unlock()
	}, nil
}

func (s *Service) wait(ctx context.Context, in WaitInput) (EventsResult, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(in.TimeoutSeconds)*time.Second+3*time.Second)
	defer cancel()
	timer := time.NewTimer(time.Duration(in.TimeoutSeconds) * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		out, e := s.events(ctx, EventsInput{RoomID: in.RoomID, AfterSeq: in.AfterSeq, Limit: 100})
		if e != nil {
			return out, e
		}
		if len(out.Data.Events) > 0 || out.Data.Status == "closed" {
			return readResult(ctx, s, "rooms.wait", out.Data)
		}
		select {
		case <-ctx.Done():
			return EventsResult{}, ctx.Err()
		case <-timer.C:
			out.Data.TimedOut = true
			return readResult(ctx, s, "rooms.wait", out.Data)
		case <-ticker.C:
		}
	}
}

func readResult[T any](ctx context.Context, s *Service, capability string, payload T) (data.QueryResult[T], error) {
	id := token.New()
	if s.Recorder != nil {
		if e := s.Recorder.Record(ctx, operations.Record{ID: id, WorkspaceID: access.From(ctx).Workspace, Capability: capability, Provider: ProviderID, Status: "succeeded", FeeUSD: "0", CreatedAt: time.Now()}); e != nil {
			return data.QueryResult[T]{}, e
		}
	}
	return data.QueryResult[T]{ID: id, Provider: ProviderID, Status: "succeeded", FeeUSD: "0", Data: payload}, nil
}
