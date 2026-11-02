package mcpoauth

import (
	"0xbot.org/api/internal/identity"
	"0xbot.org/api/internal/platform/postgres"
	"0xbot.org/api/internal/platform/token"
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestOAuthStore(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required")
	}
	u, _ := url.Parse(dsn)
	if u == nil || !strings.HasSuffix(u.Path, "_test") {
		t.Fatal("dedicated test database required")
	}
	ctx := context.Background()
	db, e := postgres.Open(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s := Store{DB: db, Resource: "https://api.test/mcp", Issuer: "https://api.test"}
	ids := identity.Store{DB: db}
	workspace := func() string {
		user, e := ids.FindOrCreate(ctx, "wallet", "0x"+token.New()[:40], "OAuth test", nil)
		if e != nil {
			t.Fatal(e)
		}
		var ws string
		if e = db.QueryRow(ctx, "SELECT id FROM workspaces WHERE owner_id=$1", user).Scan(&ws); e != nil {
			t.Fatal(e)
		}
		return ws
	}
	ws, other := workspace(), workspace()
	client, e := s.Register(ctx, Registration{Name: "Unverified test client", Redirects: []string{"http://127.0.0.1/callback"}})
	if e != nil {
		t.Fatal(e)
	}
	verifier := token.New()
	request := Authorization{ClientID: client.ID, RedirectURI: "http://127.0.0.1:9888/callback", State: "keep-state", Challenge: Challenge(verifier), Method: "S256", Resource: s.Resource, Scope: Scope, ResponseType: "code"}
	for _, change := range []func(*Authorization){func(a *Authorization) { a.Method = "plain" }, func(a *Authorization) { a.Resource = "https://other/mcp" }, func(a *Authorization) { a.Scope = "admin" }, func(a *Authorization) { a.RedirectURI = "https://evil.test/cb" }, func(a *Authorization) { a.Challenge = "short" }} {
		invalid := request
		change(&invalid)
		if _, e = s.Begin(ctx, invalid, token.New()); e == nil {
			t.Fatal("invalid authorization accepted")
		}
	}
	begin := func() (string, string) {
		browser := token.New()
		id, e := s.Begin(ctx, request, browser)
		if e != nil {
			t.Fatal(e)
		}
		return id, browser
	}
	code := func() Exchange {
		id, browser := begin()
		if _, e = s.Consent(ctx, id, "wrong"); !errors.Is(e, ErrGrant) {
			t.Fatal("browser binding bypass")
		}
		if _, e = s.Decide(ctx, id, "wrong", ws, true); !errors.Is(e, ErrGrant) {
			t.Fatal("wrong browser decision")
		}
		if _, e = s.Consent(ctx, id, browser); e != nil {
			t.Fatal(e)
		}
		redirect, e := s.Decide(ctx, id, browser, ws, true)
		if e != nil {
			t.Fatal(e)
		}
		u, e := url.Parse(redirect)
		if e != nil {
			t.Fatal(e)
		}
		if u.Query().Get("state") != "keep-state" || u.Query().Get("iss") != s.Issuer {
			t.Fatal("state or issuer lost")
		}
		if _, e = s.Decide(ctx, id, browser, ws, true); !errors.Is(e, ErrGrant) {
			t.Fatal("consent replay")
		}
		return Exchange{ClientID: client.ID, Code: u.Query().Get("code"), Verifier: verifier, RedirectURI: request.RedirectURI, Resource: s.Resource}
	}
	in := code()
	for _, change := range []func(*Exchange){func(v *Exchange) { v.Verifier = token.New() }, func(v *Exchange) { v.ClientID = "other" }, func(v *Exchange) { v.RedirectURI = "http://127.0.0.1:8888/callback" }, func(v *Exchange) { v.Resource = "https://other/mcp" }} {
		invalid := in
		change(&invalid)
		if _, e = s.ExchangeCode(ctx, invalid); !errors.Is(e, ErrGrant) {
			t.Fatal("invalid code exchange accepted")
		}
	}
	tokens, e := s.ExchangeCode(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	if got, e := s.Authenticate(ctx, tokens.AccessToken); e != nil || got != ws {
		t.Fatal("access token workspace")
	}
	if _, e = s.Authenticate(ctx, tokens.RefreshToken); !errors.Is(e, ErrGrant) {
		t.Fatal("refresh token used as access token")
	}
	otherStore := s
	otherStore.Resource = "https://other/mcp"
	if _, e = otherStore.Authenticate(ctx, tokens.AccessToken); !errors.Is(e, ErrGrant) {
		t.Fatal("audience bypass")
	}
	next, e := s.Refresh(ctx, Exchange{ClientID: client.ID, RefreshToken: tokens.RefreshToken, Resource: s.Resource})
	if e != nil {
		t.Fatal(e)
	}
	if next.RefreshToken == tokens.RefreshToken {
		t.Fatal("refresh did not rotate")
	}
	if _, e = s.Refresh(ctx, Exchange{ClientID: client.ID, RefreshToken: tokens.RefreshToken, Resource: s.Resource}); !errors.Is(e, ErrGrant) {
		t.Fatal("refresh replay accepted")
	}
	if _, e = s.Authenticate(ctx, next.AccessToken); !errors.Is(e, ErrGrant) {
		t.Fatal("replayed refresh did not revoke grant")
	}
	in = code()
	tokens, e = s.ExchangeCode(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ExchangeCode(ctx, in); !errors.Is(e, ErrGrant) {
		t.Fatal("code replay")
	}
	if _, e = s.Authenticate(ctx, tokens.AccessToken); !errors.Is(e, ErrGrant) {
		t.Fatal("code replay did not revoke issued token")
	}
	in = code()
	var success atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.ExchangeCode(ctx, in); err == nil {
				success.Add(1)
			}
		}()
	}
	wg.Wait()
	if success.Load() != 1 {
		t.Fatal("authorization code must be consumed atomically")
	}
	in = code()
	tokens, e = s.ExchangeCode(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	grants, e := s.List(ctx, ws)
	if e != nil {
		t.Fatal(e)
	}
	if len(grants) == 0 {
		t.Fatal("missing grants")
	}
	others, e := s.List(ctx, other)
	if e != nil || len(others) != 0 {
		t.Fatal("workspace isolation")
	}
	// Resolve exact grant by this test token, not list ordering.
	var grant string
	db.QueryRow(ctx, "SELECT grant_id FROM mcp_oauth_tokens WHERE hash=$1", token.Hash(tokens.AccessToken)).Scan(&grant)
	if e = s.RevokeGrant(ctx, other, grant); !errors.Is(e, ErrGrant) {
		t.Fatal("cross-workspace revoke")
	}
	if e = s.RevokeGrant(ctx, ws, grant); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Authenticate(ctx, tokens.AccessToken); !errors.Is(e, ErrGrant) {
		t.Fatal("revoked access accepted")
	}
	id, browser := begin()
	redirect, e := s.Decide(ctx, id, browser, ws, false)
	if e != nil {
		t.Fatal(e)
	}
	denied, _ := url.Parse(redirect)
	if denied.Query().Get("error") != "access_denied" || denied.Query().Get("code") != "" {
		t.Fatal("denial issued code")
	}
	id, browser = begin()
	db.Exec(ctx, "UPDATE mcp_oauth_requests SET expires_at=now()-interval '1 second' WHERE id=$1", id)
	if _, e = s.Decide(ctx, id, browser, ws, true); !errors.Is(e, ErrGrant) {
		t.Fatal("expired request")
	}
	in = code()
	db.Exec(ctx, "UPDATE mcp_oauth_codes SET expires_at=now()-interval '1 second' WHERE hash=$1", token.Hash(in.Code))
	if _, e = s.ExchangeCode(ctx, in); !errors.Is(e, ErrGrant) {
		t.Fatal("expired code")
	}
	in = code()
	tokens, e = s.ExchangeCode(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, "UPDATE mcp_oauth_tokens SET expires_at=now()-interval '1 second' WHERE hash=$1", token.Hash(tokens.AccessToken)); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Authenticate(ctx, tokens.AccessToken); !errors.Is(e, ErrGrant) {
		t.Fatal("expired access token")
	}
	success.Store(0)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Refresh(ctx, Exchange{ClientID: client.ID, RefreshToken: tokens.RefreshToken, Resource: s.Resource}); err == nil {
				success.Add(1)
			}
		}()
	}
	wg.Wait()
	if success.Load() != 1 {
		t.Fatal("refresh must rotate atomically")
	}
}
