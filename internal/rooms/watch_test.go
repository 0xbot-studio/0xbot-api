package rooms

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"0xbot.org/api/internal/access"
	"0xbot.org/api/internal/data"
)

func TestWatchClosedRoomSummaryAndCancellation(t *testing.T) {
	p := &waitProvider{r: Room{Seq: 2, Status: "closed", Events: []Event{{Seq: 1}, {Seq: 2}}}}
	s := &Service{Provider: p}
	ctx, cancel := context.WithCancel(access.With(context.Background(), access.Principal{Workspace: "viewer", Scope: "rooms:read"}))
	defer cancel()
	snapshots, heartbeats := 0, 0
	e := s.watch(ctx, EventsInput{RoomID: strings.Repeat("a", 64), AfterSeq: 1}, func(out *Result) error {
		if out == nil {
			heartbeats++
			p.mu.Lock()
			p.r.Seq = 3
			p.r.Events = append(p.r.Events, Event{Seq: 3, Kind: "summary"})
			p.r.Summary.Text = "late summary"
			p.mu.Unlock()
			return nil
		}
		snapshots++
		if out.Data.SelfID != Actor("viewer") {
			t.Fatal("incorrect identity")
		}
		if snapshots == 1 && len(out.Data.Room.Events) != 2 {
			t.Fatal("missing full replay")
		}
		if snapshots == 2 {
			if out.Data.Room.Summary.Text != "late summary" || out.Data.Room.Seq != 3 {
				t.Fatal("missing closed-room update")
			}
			cancel()
		}
		return nil
	}, time.Millisecond, time.Second)
	if !errors.Is(e, context.Canceled) || snapshots != 2 || heartbeats != 1 {
		t.Fatal(e, snapshots, heartbeats)
	}
	if s.total != 0 || len(s.waiting) != 0 {
		t.Fatal("permit leak")
	}
}

func TestWatchLimitsValidationAndRenewal(t *testing.T) {
	p := &waitProvider{r: Room{Seq: 1, Status: "closed"}}
	s := &Service{Provider: p}
	in := EventsInput{RoomID: strings.Repeat("a", 64)}
	ctx := access.With(context.Background(), access.Principal{Workspace: "viewer", Scope: "rooms:read"})
	send := func(*Result) error { return nil }
	if e := s.Watch(context.Background(), in, send); !errors.Is(e, ErrPermission) {
		t.Fatal(e)
	}
	bad := in
	bad.AfterSeq = 2
	if e := s.Watch(ctx, bad, send); !errors.Is(e, data.ErrInput) {
		t.Fatal(e)
	}
	a, _ := s.acquireWait(ctx)
	defer a()
	b, _ := s.acquireWait(ctx)
	if e := s.Watch(ctx, in, send); !errors.Is(e, data.ErrRateLimited) {
		t.Fatal(e)
	}
	if _, e := s.Wait(ctx, WaitInput{RoomID: in.RoomID}); !errors.Is(e, data.ErrRateLimited) {
		t.Fatal("shared limit", e)
	}
	b()
	count := 0
	if e := s.watch(ctx, in, func(out *Result) error {
		if out != nil {
			count++
		}
		return nil
	}, time.Millisecond, 15*time.Millisecond); e != nil {
		t.Fatal(e)
	}
	if count != 1 || s.total != 1 {
		t.Fatal("duplicate snapshots or permit leak", count, s.total)
	}
	if e := s.Watch(ctx, in, func(*Result) error { return ErrPermission }); !errors.Is(e, ErrPermission) {
		t.Fatal(e)
	}
	if s.total != 1 {
		t.Fatal("failed callback leaked permit")
	}
}
