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
	"0xbot.org/api/internal/platform/postgres"
	"0xbot.org/api/internal/platform/token"
	"0xbot.org/api/internal/providers/padinrooms"
	"0xbot.org/api/internal/rooms"
	"0xbot.org/api/internal/routing"
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRoomTransportsAndOAuth(t *testing.T) {
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
	keys := apikeys.Store{DB: db}
	settings := routing.Store{DB: db}
	provider := padinrooms.Provider{DB: db}
	s := &rooms.Service{Provider: provider, Limiter: ids, Availability: settings}
	oauth := mcpoauth.Store{DB: db, Resource: "https://api.test/mcp", Issuer: "https://api.test"}
	a := &API{Config: config.Config{AppOrigin: "https://app.test", AdminOrigin: "https://admin.test", APIOrigin: "https://api.test"}, DB: db, Identity: ids, Keys: keys, Rooms: s, OAuth: oauth, Spec: contract.Spec, Settings: settings, Router: routing.New([]data.Provider{provider}, settings)}
	a.MCP = mcpserver.NewWithRooms(nil, WorkspaceFromContextFunc, nil, s)
	h := a.Handler()
	user, e := ids.FindOrCreate(ctx, "x", token.New(), "room X account", nil)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := ids.CreateSession(ctx, user, "app")
	if e != nil {
		t.Fatal(e)
	}
	session, e := ids.Session(ctx, raw)
	if e != nil {
		t.Fatal(e)
	}
	request := func(method, path string, body any, cookie, bearer string) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, strings.NewReader(string(b)))
		r.Header.Set("Origin", a.Config.AppOrigin)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: "oxb_session", Value: cookie})
		}
		if bearer != "" {
			r.Header.Set("Authorization", "Bearer "+bearer)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	in := rooms.CreateInput{Topic: "public test", Name: "host", MaxRounds: 1, IdempotencyKey: token.New()}
	if w := request("POST", "/v1/rooms/create", in, "", ""); w.Code != 401 {
		t.Fatal("anonymous", w.Code)
	}
	key, e := keys.Create(ctx, session.User.WorkspaceID, "read-only")
	if e != nil {
		t.Fatal(e)
	}
	if w := request("POST", "/v1/rooms/create", in, "", key.Secret); w.Code != 403 {
		t.Fatal("key escalation", w.Code)
	}
	w := request("POST", "/v1/rooms/create", in, raw, "")
	if w.Code != 403 {
		t.Fatal("public creation allowed", w.Code)
	}
	adminCtx := rooms.WithAdministrator(access.With(ctx, access.Principal{Workspace: session.User.WorkspaceID, Scope: "rooms:read rooms:write"}), session.User.ID)
	made, e := s.Create(adminCtx, in)
	if e != nil {
		t.Fatal(e)
	}
	id := made.Data.Room.ID
	w = request("GET", "/v1/rooms/"+id+"/stream?after_seq=0", nil, raw, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "event: room_batch") || !strings.Contains(w.Body.String(), "created") {
		t.Fatal("SSE", w.Code)
	}
	// X-only accounts can explicitly authorize public room scopes without acquiring wallet/private-message authority.
	client, e := oauth.Register(ctx, mcpoauth.Registration{Name: "Room client", Redirects: []string{"http://127.0.0.1/callback"}})
	if e != nil {
		t.Fatal(e)
	}
	verifier, browser := token.New(), token.New()
	auth, e := oauth.Begin(ctx, mcpoauth.Authorization{ClientID: client.ID, RedirectURI: "http://127.0.0.1/callback", ResponseType: "code", State: token.New(), Challenge: mcpoauth.Challenge(verifier), Method: "S256", Resource: oauth.Resource, Scope: "data:read rooms:read rooms:write"}, browser)
	if e != nil {
		t.Fatal(e)
	}
	consent, e := oauth.Consent(ctx, auth, browser)
	if e != nil || consent.Scope != "data:read rooms:read rooms:write" {
		t.Fatal("actual scopes", e)
	}
	callback, e := oauth.Decide(ctx, auth, browser, session.User.WorkspaceID, true)
	if e != nil {
		t.Fatal(e)
	}
	cb, _ := url.Parse(callback)
	tokens, e := oauth.ExchangeCode(ctx, mcpoauth.Exchange{ClientID: client.ID, Code: cb.Query().Get("code"), Verifier: verifier, RedirectURI: "http://127.0.0.1/callback", Resource: oauth.Resource})
	if e != nil {
		t.Fatal(e)
	}
	principal, e := oauth.AuthenticateAccess(ctx, tokens.AccessToken)
	if e != nil || principal.Wallet != "" || !principal.Has("rooms:write") || principal.Has("messages:send") {
		t.Fatal("scope binding", e)
	}
	mcpCall := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "get_room", "arguments": rooms.RoomInput{RoomID: id}}}
	w = request("POST", "/mcp", mcpCall, "", tokens.AccessToken)
	if w.Code != 200 || !strings.Contains(w.Body.String(), id) || strings.Contains(w.Body.String(), `"isError":true`) {
		t.Fatal("MCP get", w.Code, w.Body.String())
	}
	mcpCall["params"] = map[string]any{"name": "create_room", "arguments": rooms.CreateInput{Topic: "MCP room", Name: "MCP host", MaxRounds: 2, IdempotencyKey: token.New()}}
	w = request("POST", "/mcp", mcpCall, "", tokens.AccessToken)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"isError":true`) {
		t.Fatal("MCP creation allowed", w.Code, w.Body.String())
	}
	if _, e = oauth.Refresh(ctx, mcpoauth.Exchange{ClientID: client.ID, RefreshToken: tokens.RefreshToken, Resource: oauth.Resource, Scope: "data:read rooms:read rooms:write messages:send"}); e == nil {
		t.Fatal("refresh escalated")
	}
	if e = oauth.RevokeToken(ctx, client.ID, tokens.AccessToken); e != nil {
		t.Fatal(e)
	}
	if w = request("POST", "/mcp", mcpCall, "", tokens.AccessToken); w.Code != 401 {
		t.Fatal("revocation", w.Code)
	}
	// Studio is protected independently from ordinary room scopes and sessions.
	wallet := "0x" + token.New()[:40]
	adminID, e := ids.FindOrCreate(ctx, "wallet", wallet, "studio fixture admin", nil)
	if e != nil {
		t.Fatal(e)
	}
	adminRaw, e := ids.CreateSession(ctx, adminID, "admin")
	if e != nil {
		t.Fatal(e)
	}
	adminSession, e := ids.Session(ctx, adminRaw)
	if e != nil {
		t.Fatal(e)
	}
	adminRequest := func(path string, body any) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", path, strings.NewReader(string(b)))
		r.Header.Set("Origin", a.Config.AdminOrigin)
		r.AddCookie(&http.Cookie{Name: "oxb_admin_session", Value: adminRaw})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	plan := rooms.StudioCreateInput{CreateInput: rooms.CreateInput{Topic: "Studio privacy fixture", Name: "operator", MaxRounds: 1, IdempotencyKey: token.New()}, Plan: rooms.StudioPlan{Version: 1, CommonPrompt: "PRIVATE_RULES_FIXTURE", BudgetMicroUSD: "1000000", Guests: []rooms.GuestPlan{{Name: "skeptic", Role: "counterargument", Provider: "fixture-not-live", Model: "fixture-model", SystemPrompt: "PRIVATE_PROMPT_FIXTURE", MaxOutputTokens: 256}, {Name: "optimist", Role: "opportunity", Provider: "other-fixture", Model: "other-model", SystemPrompt: "PRIVATE_ALTERNATE_FIXTURE", MaxOutputTokens: 256}}}}
	if w = adminRequest("/v1/admin/rooms/create", plan); w.Code != 403 {
		t.Fatal("allowlist bypass", w.Code)
	}
	a.Config.AdminWallets = map[string]bool{wallet: true}
	if w = adminRequest("/v1/admin/rooms/create", plan); w.Code != 403 || !strings.Contains(w.Body.String(), "mfa_required") {
		t.Fatal("MFA bypass", w.Code)
	}
	if e = ids.MFA(ctx, adminSession.Hash); e != nil {
		t.Fatal(e)
	}
	w = adminRequest("/v1/admin/rooms/create", plan)
	var created rooms.Result
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &created) != nil {
		t.Fatal("studio create", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "PRIVATE_") {
		t.Fatal("private plan in public result")
	}
	w = adminRequest("/v1/admin/rooms/create", plan)
	var replay rooms.Result
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &replay) != nil || replay.ID != created.ID {
		t.Fatal("studio idempotency")
	}
	plan.Plan.Guests[0].SystemPrompt = "changed"
	if w = adminRequest("/v1/admin/rooms/create", plan); w.Code != 409 {
		t.Fatal("plan conflict", w.Code)
	}
	input := rooms.RoomInput{RoomID: created.Data.Room.ID}
	w = adminRequest("/v1/admin/rooms/get", input)
	var private rooms.StudioDetail
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &private) != nil || private.Plan == nil || len(private.Plan.Guests) != 2 {
		t.Fatal("private plan missing")
	}
	if private.Plan.Guests[0].AgentID == private.Plan.Guests[1].AgentID || !rooms.IDPattern.MatchString(private.Plan.Guests[0].AgentID) {
		t.Fatal("agent identities")
	}
	if private.Plan.Guests[0].SystemPrompt != "PRIVATE_PROMPT_FIXTURE" || len(private.Snapshot.Room.Members) != 1 || private.ExecutionStatus != "external_runner_required" {
		t.Fatal("plan changed or fake execution")
	}
	if w = request("POST", "/v1/admin/rooms/get", input, raw, ""); w.Code != 403 {
		t.Fatal("app read private plan", w.Code)
	}
	w = request("POST", "/v1/rooms/get", input, raw, "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "PRIVATE_") || strings.Contains(w.Body.String(), "system_prompt") {
		t.Fatal("public plan leak", w.Code)
	}
	var auditCount int
	if e = db.QueryRow(ctx, "SELECT count(*) FROM audit_log WHERE target=$1 AND action='rooms.create' AND details::text NOT LIKE '%PRIVATE_%'", input.RoomID).Scan(&auditCount); e != nil || auditCount != 1 {
		t.Fatal("atomic audit dedup", auditCount, e)
	}
	if e = ids.Revoke(ctx, adminSession.Hash); e != nil {
		t.Fatal(e)
	}
	if w = adminRequest("/v1/admin/rooms/get", input); w.Code != 401 {
		t.Fatal("revoked admin", w.Code)
	}
	// Real HTTP streaming: the first frame must flush before the handler ends.
	server := httptest.NewServer(h)
	defer server.Close()
	streamCtx, stopStream := context.WithTimeout(ctx, 12*time.Second)
	defer stopStream()
	req, _ := http.NewRequestWithContext(streamCtx, "GET", server.URL+"/v1/rooms/"+id+"/stream?after_seq=0&live=true", nil)
	req.Header.Set("Origin", a.Config.AppOrigin)
	req.AddCookie(&http.Cookie{Name: "oxb_session", Value: raw})
	started := time.Now()
	response, e := server.Client().Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	frame := func(event string) string {
		t.Helper()
		found := false
		for scanner.Scan() {
			line := scanner.Text()
			if line == "event: "+event {
				found = true
			}
			if found && strings.HasPrefix(line, "data: ") {
				return strings.TrimPrefix(line, "data: ")
			}
		}
		t.Fatalf("stream missing %s: %v", event, scanner.Err())
		return ""
	}
	initial := frame("room_snapshot")
	if response.StatusCode != 200 || time.Since(started) > 2*time.Second || !strings.Contains(initial, id) {
		t.Fatal("not flushed", response.StatusCode)
	}
	closed, e := s.Moderate(adminCtx, rooms.ModerateInput{RoomID: id, Action: "close", ExpectedSeq: made.Data.Room.Seq, IdempotencyKey: token.New()})
	if e != nil {
		t.Fatal(e)
	}
	var pushed rooms.Result
	if json.Unmarshal([]byte(frame("room_snapshot")), &pushed) != nil || pushed.Data.Room.Seq != closed.Data.Room.Seq || pushed.Data.Room.Status != "closed" {
		t.Fatal("state not pushed")
	}
	if e = ids.Revoke(ctx, session.Hash); e != nil {
		t.Fatal(e)
	}
	if body := frame("room_error"); !strings.Contains(body, "authentication_required") {
		t.Fatal("revoked stream continued")
	}
	stopStream()
	if w = request("POST", "/v1/rooms/get", rooms.RoomInput{RoomID: id}, raw, ""); w.Code != 401 {
		t.Fatal("session revocation", w.Code)
	}
}
func WorkspaceFromContextFunc(ctx context.Context) string { return WorkspaceFromContext(ctx) }
