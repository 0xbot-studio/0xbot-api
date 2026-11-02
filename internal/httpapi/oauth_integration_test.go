package httpapi

import (
	"0xbot.org/api/internal/apikeys"
	"0xbot.org/api/internal/config"
	"0xbot.org/api/internal/identity"
	"0xbot.org/api/internal/mcpoauth"
	"0xbot.org/api/internal/mcpserver"
	"0xbot.org/api/internal/platform/postgres"
	"0xbot.org/api/internal/platform/token"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestMCPOAuthHTTP(t *testing.T) {
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
	user, e := ids.FindOrCreate(ctx, "wallet", "0x"+token.New()[:40], "OAuth HTTP test", nil)
	if e != nil {
		t.Fatal(e)
	}
	session, e := ids.CreateSession(ctx, user, "app")
	if e != nil {
		t.Fatal(e)
	}
	cookie := &http.Cookie{Name: "oxb_session", Value: session}
	a := &API{Config: config.Config{APIOrigin: "https://api.test", AppOrigin: "https://app.test", AdminOrigin: "https://admin.test", Secure: true}, DB: db, Identity: ids, Keys: apikeys.Store{DB: db}, OAuth: mcpoauth.Store{DB: db, Resource: "https://api.test/mcp", Issuer: "https://api.test"}}
	a.MCP = mcpserver.New(nil, func(ctx context.Context) string { return WorkspaceFromContext(ctx) })
	handler := a.Handler()
	call := func(method, path, body, contentType, origin, bearer string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.RemoteAddr = "test-" + token.New()[:8] + ":1"
		r.Header.Set("Content-Type", contentType)
		r.Header.Set("Accept", "application/json, text/event-stream")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if bearer != "" {
			r.Header.Set("Authorization", "Bearer "+bearer)
		}
		for _, c := range cookies {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	check := func(w *httptest.ResponseRecorder, status int) {
		t.Helper()
		if w.Code != status {
			t.Fatalf("expected HTTP %d, got %d", status, w.Code)
		}
	}
	noauth := call("POST", "/mcp", "{}", "application/json", "", "")
	check(noauth, 401)
	if !strings.Contains(noauth.Header().Get("WWW-Authenticate"), "oauth-protected-resource/mcp") {
		t.Fatal("missing OAuth discovery challenge")
	}
	check(call("POST", "/mcp", "{}", "application/json", "https://evil.test", "", cookie), 401)
	for _, path := range []string{"/.well-known/oauth-protected-resource/mcp", "/.well-known/oauth-authorization-server"} {
		w := call("GET", path, "", "", "https://client.test", "")
		check(w, 200)
		if w.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Fatal("metadata CORS")
		}
	}
	reg := call("POST", "/oauth/register", `{"client_name":"Test client","redirect_uris":["http://127.0.0.1/callback"],"token_endpoint_auth_method":"none","client_uri":"https://self-declared.test"}`, "application/json", "", "")
	check(reg, 201)
	var client mcpoauth.Client
	if e = json.Unmarshal(reg.Body.Bytes(), &client); e != nil {
		t.Fatal(e)
	}
	verifier := token.New()
	params := url.Values{"response_type": {"code"}, "client_id": {client.ID}, "redirect_uri": {"http://127.0.0.1:9811/callback"}, "resource": {"https://api.test/mcp"}, "scope": {"data:read"}, "state": {"opaque-client-state"}, "code_challenge": {mcpoauth.Challenge(verifier)}, "code_challenge_method": {"S256"}}
	auth := call("GET", "/oauth/authorize?"+params.Encode(), "", "", "", "")
	check(auth, 302)
	loc, e := url.Parse(auth.Header().Get("Location"))
	if e != nil || loc.Host != "app.test" {
		t.Fatal("wrong consent redirect")
	}
	id := strings.TrimPrefix(loc.Fragment, "oauth/")
	var flow *http.Cookie
	for _, c := range auth.Result().Cookies() {
		if c.Name == "__Host-oxb_mcp_flow" {
			flow = c
		}
	}
	if flow == nil || !flow.HttpOnly || !flow.Secure || flow.SameSite != http.SameSiteLaxMode {
		t.Fatal("unsafe browser binding cookie")
	}
	path := "/v1/oauth/requests/" + id
	check(call("GET", path, "", "", "https://app.test", "", flow), 401)
	check(call("GET", path, "", "", "https://app.test", "", cookie), 400)
	consent := call("GET", path, "", "", "https://app.test", "", cookie, flow)
	check(consent, 200)
	var details mcpoauth.Consent
	json.Unmarshal(consent.Body.Bytes(), &details)
	if details.Scope != "data:read" || details.ClientID != client.ID {
		t.Fatal("consent details")
	}
	check(call("POST", path, `{"approve":true}`, "application/json", "https://evil.test", "", cookie, flow), 403)
	check(call("POST", path, `{"approve":true}`, "application/json", "", "", cookie, flow), 403)
	approval := call("POST", path, `{"approve":true}`, "application/json", "https://app.test", "", cookie, flow)
	check(approval, 200)
	var approved struct {
		Redirect string `json:"redirect_uri"`
	}
	json.Unmarshal(approval.Body.Bytes(), &approved)
	callback, e := url.Parse(approved.Redirect)
	if e != nil {
		t.Fatal(e)
	}
	if callback.Query().Get("state") != "opaque-client-state" || callback.Query().Get("iss") != "https://api.test" {
		t.Fatal("state/issuer")
	}
	form := url.Values{"grant_type": {"authorization_code"}, "client_id": {client.ID}, "code": {callback.Query().Get("code")}, "redirect_uri": {params.Get("redirect_uri")}, "resource": {"https://api.test/mcp"}, "code_verifier": {verifier}}
	exchanged := call("POST", "/oauth/token", form.Encode(), "application/x-www-form-urlencoded", "", "")
	check(exchanged, 200)
	var tokens mcpoauth.Tokens
	json.Unmarshal(exchanged.Body.Bytes(), &tokens)
	if tokens.AccessToken == "" || tokens.RefreshToken == "" || tokens.Scope != "data:read" {
		t.Fatal("missing tokens")
	}
	if exchanged.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("token cacheable")
	}
	init := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"oauth-test","version":"1"}}}`
	check(call("POST", "/mcp", init, "application/json", "", ""+tokens.AccessToken), 200)
	toolReply := call("POST", "/mcp", `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_supported_tokens","arguments":{}}}`, "application/json", "", tokens.AccessToken)
	check(toolReply, 200)
	if !strings.Contains(toolReply.Body.String(), "USDT") {
		t.Fatal("OAuth token did not reach MCP tool")
	}
	check(call("POST", "/v1/data/balance", "{}", "application/json", "", tokens.AccessToken), 401)
	check(call("GET", "/v1/keys", "", "", "", tokens.AccessToken), 401)
	check(call("GET", "/v1/admin/providers", "", "", "", tokens.AccessToken), 401)
	revoked := url.Values{"client_id": {client.ID}, "token": {tokens.RefreshToken}}
	check(call("POST", "/oauth/revoke", revoked.Encode(), "application/x-www-form-urlencoded", "", ""), 200)
	check(call("POST", "/mcp", init, "application/json", "", tokens.AccessToken), 401)
	refresh := url.Values{"grant_type": {"refresh_token"}, "client_id": {client.ID}, "refresh_token": {tokens.RefreshToken}, "resource": {"https://api.test/mcp"}}
	failed := call("POST", "/oauth/token", refresh.Encode(), "application/x-www-form-urlencoded", "", "")
	check(failed, 400)
	var errBody map[string]string
	json.Unmarshal(failed.Body.Bytes(), &errBody)
	if errBody["error"] != "invalid_grant" {
		t.Fatal("OAuth error format")
	}
	bad := call("POST", "/oauth/register", `{"redirect_uris":["https://good.test/callback"],"token_endpoint_auth_method":"client_secret_basic"}`, "application/json", "", "")
	check(bad, 400)
}
