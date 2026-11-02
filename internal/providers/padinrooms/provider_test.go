package padinrooms

import (
	"0xbot.org/api/internal/access"
	"0xbot.org/api/internal/identity"
	"0xbot.org/api/internal/platform/postgres"
	"0xbot.org/api/internal/platform/token"
	"0xbot.org/api/internal/rooms"
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPersistentRooms(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required")
	}
	u, _ := url.Parse(dsn)
	if u == nil || !strings.HasSuffix(u.Path, "_test") {
		t.Fatal("dedicated test DB required")
	}
	ctx := context.Background()
	db, e := postgres.Open(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	ids := identity.Store{DB: db}
	p := Provider{DB: db}
	s := &rooms.Service{Provider: p}
	users := map[string]string{}
	account := func() context.Context {
		id, e := ids.FindOrCreate(ctx, "wallet", "0x"+token.New()[:40], "room test", nil)
		if e != nil {
			t.Fatal(e)
		}
		raw, e := ids.CreateSession(ctx, id, "app")
		if e != nil {
			t.Fatal(e)
		}
		session, e := ids.Session(ctx, raw)
		if e != nil {
			t.Fatal(e)
		}
		users[session.User.WorkspaceID] = session.User.ID
		return access.With(ctx, access.Principal{Workspace: session.User.WorkspaceID, Scope: "rooms:read rooms:write"})
	}
	host, a, b := account(), account(), account()
	host = rooms.WithAdministrator(host, users[access.From(host).Workspace])
	in := rooms.CreateInput{Topic: "test discussion", Name: "host", MaxRounds: 1, IdempotencyKey: token.New()}
	out, e := s.Create(host, in)
	if e != nil {
		t.Fatal(e)
	}
	id := out.Data.Room.ID
	repeat, e := s.Create(host, in)
	if e != nil || repeat.ID != out.ID {
		t.Fatal("create idempotency", e)
	}
	in.Topic = "changed"
	if _, e = s.Create(host, in); !errors.Is(e, rooms.ErrConflict) {
		t.Fatal("conflict", e)
	}
	for _, actor := range []context.Context{a, b} {
		_, e = s.Join(actor, rooms.JoinInput{RoomID: id, Name: "guest", IdempotencyKey: token.New()})
		if e != nil {
			t.Fatal(e)
		}
	}
	moderate := func(action, member string) {
		r, e := s.Get(host, rooms.RoomInput{RoomID: id})
		if e != nil {
			t.Fatal(e)
		}
		_, e = s.Moderate(host, rooms.ModerateInput{RoomID: id, Action: action, ParticipantID: member, ExpectedSeq: r.Data.Room.Seq, IdempotencyKey: token.New()})
		if e != nil {
			t.Fatal(e)
		}
	}
	moderate("promote", rooms.Actor(access.From(a).Workspace))
	moderate("promote", rooms.Actor(access.From(b).Workspace))
	moderate("start", "")
	post := rooms.PostInput{RoomID: id, Body: "one reply", ExpectedTurn: 1, IdempotencyKey: token.New()}
	var wg sync.WaitGroup
	results := make(chan rooms.Result, 6)
	errs := make(chan error, 6)
	for range 6 {
		wg.Add(1)
		go func() { defer wg.Done(); r, e := s.Post(a, post); results <- r; errs <- e }()
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	op := ""
	for r := range results {
		if op != "" && op != r.ID {
			t.Fatal("different receipt")
		}
		op = r.ID
	}
	var count int
	if e = db.QueryRow(ctx, "SELECT count(*) FROM operations WHERE id=$1", op).Scan(&count); e != nil || count != 1 {
		t.Fatal("operation dedup", count, e)
	}
	snap, e := s.Get(host, rooms.RoomInput{RoomID: id})
	if e != nil {
		t.Fatal(e)
	}
	waitCtx, cancel := context.WithCancel(a)
	done := make(chan error, 1)
	go func() {
		_, e := s.Wait(waitCtx, rooms.WaitInput{RoomID: id, AfterSeq: snap.Data.Room.Seq, TimeoutSeconds: 20})
		done <- e
	}()
	cancel()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("cancellation ignored")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("wait leaked")
	}
	page, e := s.Wait(a, rooms.WaitInput{RoomID: id, AfterSeq: snap.Data.Room.Seq, TimeoutSeconds: 1})
	if e != nil || !page.Data.TimedOut {
		t.Fatal("wait timeout", e)
	}
	_, e = s.Post(b, rooms.PostInput{RoomID: id, Body: "last reply", ExpectedTurn: 2, IdempotencyKey: token.New()})
	if e != nil {
		t.Fatal(e)
	}
	// Fresh provider instance observes persisted state, not an in-memory event bus.
	restored, e := (Provider{DB: db}).Get(ctx, id)
	if e != nil || restored.Status != "closed" || len(restored.Summary.MessageSeqs) != 2 {
		t.Fatal("persistence", e)
	}
	page, e = s.Wait(a, rooms.WaitInput{RoomID: id, AfterSeq: restored.Seq, TimeoutSeconds: 20})
	if e != nil || page.Data.Status != "closed" || page.Data.TimedOut {
		t.Fatal("closed wait", e)
	}
	readonly := access.With(ctx, access.Principal{Workspace: access.From(a).Workspace, Scope: "data:read"})
	if _, e = s.Get(readonly, rooms.RoomInput{RoomID: id}); !errors.Is(e, rooms.ErrPermission) {
		t.Fatal("old scope", e)
	}
	if _, e = s.Join(readonly, rooms.JoinInput{RoomID: id, Name: "x", IdempotencyKey: token.New()}); !errors.Is(e, rooms.ErrPermission) {
		t.Fatal("write scope", e)
	}
	final := rooms.SummaryInput{RoomID: id, Body: "A cited synthesis", ThroughSeq: restored.Seq, MessageSeqs: restored.Summary.MessageSeqs, IdempotencyKey: token.New()}
	if _, e = s.SubmitSummary(b, final); !errors.Is(e, rooms.ErrPermission) {
		t.Fatal("summary host boundary", e)
	}
	summary, e := s.SubmitSummary(host, final)
	if e != nil || summary.Data.Room.Summary.Kind != "submitted" {
		t.Fatal("summary persisted", e)
	}
	again, e := s.SubmitSummary(host, final)
	if e != nil || again.ID != summary.ID {
		t.Fatal("summary receipt", e)
	}
	final.Body = "changed"
	if _, e = s.SubmitSummary(host, final); !errors.Is(e, rooms.ErrConflict) {
		t.Fatal("summary conflict", e)
	}
}
