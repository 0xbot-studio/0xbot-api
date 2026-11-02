package httpapi

import (
	"0xbot.org/api/internal/access"
	"0xbot.org/api/internal/apikeys"
	"0xbot.org/api/internal/config"
	"0xbot.org/api/internal/contract"
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/identity"
	"0xbot.org/api/internal/mcpoauth"
	"0xbot.org/api/internal/mcpserver"
	"0xbot.org/api/internal/messaging"
	"0xbot.org/api/internal/operations"
	"0xbot.org/api/internal/platform/postgres"
	"0xbot.org/api/internal/platform/token"
	"0xbot.org/api/internal/providers/padininbox"
	"0xbot.org/api/internal/routing"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/ethereum/go-ethereum/crypto"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestPrivateMessages(t *testing.T) {
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
	ids := identity.Store{DB: db}
	keys := apikeys.Store{DB: db}
	settings := routing.Store{DB: db}
	provider := padininbox.Provider{DB: db}
	messages := &messaging.Service{Provider: provider, Identity: ids, Limiter: ids, Availability: settings, Recorder: operations.Store{DB: db}}
	oauth := mcpoauth.Store{DB: db, Resource: "https://api.test/mcp", Issuer: "https://api.test"}
	a := &API{Config: config.Config{AppOrigin: "https://app.test", AdminOrigin: "https://admin.test", APIOrigin: "https://api.test", AdminWallets: map[string]bool{}}, DB: db, Identity: ids, Keys: keys, Messages: messages, OAuth: oauth, Spec: contract.Spec, Settings: settings, Router: routing.New([]data.Provider{provider}, settings)}
	a.MCP = mcpserver.New(nil, func(ctx context.Context) string { return WorkspaceFromContext(ctx) }, messages)
	handler := a.Handler()
	remote := token.New()
	request := func(path string, body any, cookie *http.Cookie, bearer string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", path, strings.NewReader(string(raw)))
		r.RemoteAddr = "test-" + remote + ":8080"
		r.Header.Set("Origin", a.Config.AppOrigin)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		if bearer != "" {
			r.Header.Set("Authorization", "Bearer "+bearer)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	type account struct {
		address, workspace string
		cookie             *http.Cookie
	}
	login := func() account {
		key, e := crypto.GenerateKey()
		if e != nil {
			t.Fatal(e)
		}
		address := strings.ToLower(crypto.PubkeyToAddress(key.PublicKey).Hex())
		r := request("/v1/auth/wallet/challenge", map[string]string{"address": address}, nil, "")
		var c struct{ ID, Message string }
		if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &c) != nil {
			t.Fatal("challenge failed")
		}
		hash := crypto.Keccak256([]byte(fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(c.Message), c.Message)))
		sig, _ := crypto.Sign(hash, key)
		sig[64] += 27
		r = request("/v1/auth/wallet/verify", map[string]string{"id": c.ID, "signature": "0x" + hex.EncodeToString(sig)}, nil, "")
		if r.Code != 200 {
			t.Fatal(r.Body.String())
		}
		cookie := r.Result().Cookies()[0]
		s, e := ids.Session(ctx, cookie.Value)
		if e != nil {
			t.Fatal(e)
		}
		return account{address, s.User.WorkspaceID, cookie}
	}
	sender, recipient, outsider := login(), login(), login()
	// X-only account authority cannot be used to claim an arbitrary wallet inbox.
	xUser, err := ids.FindOrCreate(ctx, "x", token.New(), "X-only test", nil)
	if err != nil {
		t.Fatal(err)
	}
	var xWorkspace string
	if err = db.QueryRow(ctx, "SELECT id FROM workspaces WHERE owner_id=$1", xUser).Scan(&xWorkspace); err != nil {
		t.Fatal(err)
	}
	if _, err = messages.List(access.With(ctx, access.Principal{Workspace: xWorkspace, Scope: "messages:read"}), messaging.ListInput{Address: recipient.address}); err != messaging.ErrPermission {
		t.Fatal("X-only account claimed wallet", err)
	}
	in := messaging.SendInput{From: sender.address, To: recipient.address, Body: "private-body-" + token.New(), IdempotencyKey: token.New()}
	expect := func(r *httptest.ResponseRecorder, status int) {
		t.Helper()
		if r.Code != status {
			t.Fatalf("want %d got %d: %s", status, r.Code, r.Body.String())
		}
	}
	expect(request("/v1/messages/send", in, nil, ""), 401)
	key, e := keys.Create(ctx, sender.workspace, "read-only")
	if e != nil {
		t.Fatal(e)
	}
	expect(request("/v1/messages/send", in, nil, key.Secret), 403)
	expect(request("/v1/messages/send", in, outsider.cookie, ""), 403)
	sent := request("/v1/messages/send", in, sender.cookie, "")
	expect(sent, 200)
	var message messaging.Result
	json.Unmarshal(sent.Body.Bytes(), &message)
	if message.Data.Status != "stored" || message.Data.Body != in.Body {
		t.Fatal("send mismatch")
	}
	retry := request("/v1/messages/send", in, sender.cookie, "")
	expect(retry, 200)
	var repeated messaging.Result
	json.Unmarshal(retry.Body.Bytes(), &repeated)
	if repeated.ID != message.ID {
		t.Fatal("duplicate send")
	}
	changed := in
	changed.Body = "different"
	expect(request("/v1/messages/send", changed, sender.cookie, ""), 409)
	list := messaging.ListInput{Address: recipient.address, Limit: 1}
	expect(request("/v1/messages/list", list, sender.cookie, ""), 403)
	inbox := request("/v1/messages/list", list, recipient.cookie, "")
	expect(inbox, 200)
	if strings.Contains(inbox.Body.String(), in.Body) {
		t.Fatal("body leaked in list")
	}
	detail := messaging.GetInput{Address: recipient.address, ID: message.Data.ID}
	expect(request("/v1/messages/get", detail, recipient.cookie, ""), 200)
	detail.Address = outsider.address
	r := request("/v1/messages/get", detail, outsider.cookie, "")
	expect(r, 404)
	if strings.Contains(r.Body.String(), in.Body) {
		t.Fatal("other inbox leaked")
	}
	// Concurrent retries share a single durable send and operation record.
	concurrent := in
	concurrent.IdempotencyKey = token.New()
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := provider.Send(ctx, sender.workspace, concurrent); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int
	e = db.QueryRow(ctx, "SELECT count(*) FROM padin_messages WHERE workspace_id=$1", sender.workspace).Scan(&count)
	if e != nil || count != 2 {
		t.Fatal("send deduplication", count, e)
	}
	e = db.QueryRow(ctx, "SELECT count(*) FROM operations WHERE workspace_id=$1 AND capability='messaging.send'", sender.workspace).Scan(&count)
	if e != nil || count != 2 {
		t.Fatal("usage deduplication", count, e)
	}
	first, e := provider.List(ctx, list)
	if e != nil || first.NextCursor == "" {
		t.Fatal("pagination", e)
	}
	list.Cursor = first.NextCursor
	second, e := provider.List(ctx, list)
	if e != nil || len(second.Messages) != 1 || second.Messages[0].ID == first.Messages[0].ID {
		t.Fatal("pagination duplicate", e)
	}
	list.Address = outsider.address
	if _, e = provider.List(ctx, list); e == nil {
		t.Fatal("foreign cursor accepted")
	}
	// Unregistered recipients are accepted, with no external notification claim.
	unknown := in
	unknown.To = "0x" + token.New()[:40]
	unknown.IdempotencyKey = token.New()
	expect(request("/v1/messages/send", unknown, sender.cookie, ""), 200)
	// Scoped OAuth must be explicitly approved for an owned wallet, without escalating old grants.
	client, e := oauth.Register(ctx, mcpoauth.Registration{Name: "Message test", Redirects: []string{"http://127.0.0.1/cb"}})
	if e != nil {
		t.Fatal(e)
	}
	grant := func(scope string, acct account) mcpoauth.Tokens {
		browser, verifier := token.New(), token.New()
		auth := mcpoauth.Authorization{ClientID: client.ID, RedirectURI: "http://127.0.0.1/cb", Resource: oauth.Resource, Scope: scope, Challenge: mcpoauth.Challenge(verifier), Method: "S256", ResponseType: "code"}
		id, e := oauth.Begin(ctx, auth, browser)
		if e != nil {
			t.Fatal(e)
		}
		c, e := oauth.Consent(ctx, id, browser)
		if e != nil || c.Scope != scope {
			t.Fatal("wrong consent scope", c, e)
		}
		if scope != "data:read" {
			if _, e = oauth.DecideWallet(ctx, id, browser, acct.workspace, true, outsider.address); e == nil {
				t.Fatal("foreign wallet granted")
			}
		}
		// Exercise the browser-bound consent HTTP handler, including selected wallet.
		approve := func(wallet string) *httptest.ResponseRecorder {
			raw, _ := json.Marshal(map[string]any{"approve": true, "wallet": wallet})
			r := httptest.NewRequest("POST", "/v1/oauth/requests/"+id, strings.NewReader(string(raw)))
			r.Header.Set("Origin", a.Config.AppOrigin)
			r.Header.Set("Content-Type", "application/json")
			r.AddCookie(acct.cookie)
			r.AddCookie(&http.Cookie{Name: a.flowCookieName(), Value: id + "." + browser})
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			return w
		}
		if scope != "data:read" {
			expect(approve(""), 400)
		}
		approved := approve(acct.address)
		expect(approved, 200)
		var decision struct {
			Redirect string `json:"redirect_uri"`
		}
		if e = json.Unmarshal(approved.Body.Bytes(), &decision); e != nil {
			t.Fatal(e)
		}
		u, _ := url.Parse(decision.Redirect)
		tokens, e := oauth.ExchangeCode(ctx, mcpoauth.Exchange{ClientID: client.ID, Code: u.Query().Get("code"), RedirectURI: auth.RedirectURI, Verifier: verifier, Resource: oauth.Resource})
		if e != nil || tokens.Scope != scope {
			t.Fatal("wrong issued scope", e)
		}
		return tokens
	}
	mcpCall := func(tool string, args any, bearer string) *httptest.ResponseRecorder {
		return request("/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": tool, "arguments": args}}, nil, bearer)
	}
	old := grant("data:read", recipient)
	detail.Address = recipient.address
	if r = mcpCall("get_message", detail, old.AccessToken); !strings.Contains(r.Body.String(), "message_permission_required") || strings.Contains(r.Body.String(), in.Body) {
		t.Fatal("old token gained private access")
	}
	read := grant("data:read messages:read", recipient)
	r = mcpCall("get_message", detail, read.AccessToken)
	if r.Code != 200 || !strings.Contains(r.Body.String(), in.Body) {
		t.Fatal("MCP detail failed", r.Body.String())
	}
	spoof := in
	spoof.From = recipient.address
	if r = mcpCall("send_message", spoof, read.AccessToken); !strings.Contains(r.Body.String(), "message_permission_required") {
		t.Fatal("read token sent")
	}
	send := grant("data:read messages:send", sender)
	extra := in
	extra.IdempotencyKey = token.New()
	r = mcpCall("send_message", extra, send.AccessToken)
	if r.Code != 200 || !strings.Contains(r.Body.String(), "stored") {
		t.Fatal("MCP send failed", r.Body.String())
	}
	_, e = oauth.Refresh(ctx, mcpoauth.Exchange{ClientID: client.ID, RefreshToken: read.RefreshToken, Resource: oauth.Resource, Scope: "data:read messages:read messages:send"})
	if e == nil {
		t.Fatal("refresh escalated scope")
	}
	refreshed, e := oauth.Refresh(ctx, mcpoauth.Exchange{ClientID: client.ID, RefreshToken: read.RefreshToken, Resource: oauth.Resource})
	if e != nil || refreshed.Scope != read.Scope {
		t.Fatal("refresh scope drift", e)
	}
	principal, e := oauth.AuthenticateAccess(ctx, refreshed.AccessToken)
	if e != nil || principal.Wallet != recipient.address {
		t.Fatal("wallet binding lost", e)
	}
	wrongCtx := access.With(ctx, access.Principal{Workspace: recipient.workspace, Scope: "data:read messages:read", Wallet: outsider.address})
	if _, e = messages.Get(wrongCtx, detail); e == nil {
		t.Fatal("token wallet binding bypass")
	}
	if e = oauth.RevokeToken(ctx, client.ID, refreshed.AccessToken); e != nil {
		t.Fatal(e)
	}
	expect(mcpCall("get_message", detail, refreshed.AccessToken), 401)
	// Send quota is atomic and retries remain valid at the limit.
	for i := 0; i < 6; i++ {
		next := in
		next.IdempotencyKey = token.New()
		if _, e = provider.Send(ctx, sender.workspace, next); e != nil {
			t.Fatal(e)
		}
	}
	next := in
	next.IdempotencyKey = token.New()
	if _, e = provider.Send(ctx, sender.workspace, next); e != data.ErrRateLimited {
		t.Fatal("send quota", e)
	}
	if _, e = provider.Send(ctx, sender.workspace, in); e != nil {
		t.Fatal("idempotent retry blocked at send quota", e)
	}
}
