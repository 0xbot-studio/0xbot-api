package rooms

import (
	"0xbot.org/api/internal/access"
	"0xbot.org/api/internal/data"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type waitProvider struct {
	mu sync.Mutex
	r  Room
}

func (p *waitProvider) Get(ctx context.Context, id string) (Room, error) {
	if e := ctx.Err(); e != nil {
		return Room{}, e
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	r := p.r
	r.Events = append([]Event{}, r.Events...)
	return r, nil
}
func (p *waitProvider) List(context.Context, string, int) (Listing, error) { return Listing{}, nil }
func (p *waitProvider) Mutate(context.Context, string, string, Command) (Result, error) {
	return Result{}, nil
}
func TestWaitDeliveryAndBound(t *testing.T) {
	p := &waitProvider{r: Room{Status: "live", Events: []Event{}}}
	s := &Service{Provider: p}
	ctx, cancel := context.WithCancel(access.With(context.Background(), access.Principal{Workspace: "a", Scope: "rooms:read"}))
	defer cancel()
	in := WaitInput{RoomID: strings.Repeat("a", 64), TimeoutSeconds: 3}
	results := make(chan EventsResult, 2)
	errs := make(chan error, 2)
	for range 2 {
		go func() { r, e := s.Wait(ctx, in); results <- r; errs <- e }()
	}
	deadline := time.Now().Add(time.Second)
	for {
		s.mu.Lock()
		count := s.total
		s.mu.Unlock()
		if count == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("waits did not start")
		}
		time.Sleep(time.Millisecond)
	}
	if _, e := s.Wait(ctx, in); !errors.Is(e, data.ErrRateLimited) {
		t.Fatal("concurrency cap", e)
	}
	p.mu.Lock()
	p.r.Seq = 1
	p.r.Events = []Event{{Seq: 1, Kind: "message", Body: "new"}}
	p.mu.Unlock()
	for range 2 {
		select {
		case r := <-results:
			if r.Data.NextSeq != 1 || len(r.Data.Events) != 1 {
				t.Fatal(r)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("no wakeup")
		}
		if e := <-errs; e != nil {
			t.Fatal(e)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.total != 0 || len(s.waiting) != 0 {
		t.Fatal("wait permits leaked")
	}
}
