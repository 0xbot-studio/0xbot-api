package main

import (
	"0xbot.org/api/internal/mcpoauth"
	"0xbot.org/api/internal/platform/token"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// oauthSmoke uses the existing unfunded smoke identity. It simulates browser
// consent over HTTP; it does not certify a third-party client's browser UI.
func oauthSmoke(client *http.Client, api, app, address string) error {
	browser := *client
	browser.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	call := func(method, path, typ string, body []byte, status int, out any) (http.Header, error) {
		req, e := http.NewRequest(method, api+path, bytes.NewReader(body))
		if e != nil {
			return nil, e
		}
		req.Header.Set("Origin", app)
		if typ != "" {
			req.Header.Set("Content-Type", typ)
		}
		res, e := browser.Do(req)
		if e != nil {
			return nil, fmt.Errorf("OAuth smoke transport failed")
		}
		defer res.Body.Close()
		if res.StatusCode != status {
			return nil, fmt.Errorf("OAuth smoke %s: HTTP %d", path, res.StatusCode)
		}
		if out != nil {
			if e = json.NewDecoder(res.Body).Decode(out); e != nil {
				return nil, e
			}
		}
		return res.Header, nil
	}
	var clientInfo mcpoauth.Client
	raw, _ := json.Marshal(mcpoauth.Registration{Name: "0xbot deployment verification", Redirects: []string{"http://127.0.0.1/callback"}})
	if _, e := call("POST", "/oauth/register", "application/json", raw, 201, &clientInfo); e != nil {
		return e
	}
	verifier := token.New()
	params := url.Values{"response_type": {"code"}, "client_id": {clientInfo.ID}, "redirect_uri": {"http://127.0.0.1:9811/callback"}, "resource": {api + "/mcp"}, "scope": {"data:read"}, "state": {token.New()}, "code_challenge": {mcpoauth.Challenge(verifier)}, "code_challenge_method": {"S256"}}
	headers, e := call("GET", "/oauth/authorize?"+params.Encode(), "", nil, 302, nil)
	if e != nil {
		return fmt.Errorf("OAuth authorization did not redirect")
	}
	location, e := url.Parse(headers.Get("Location"))
	if e != nil {
		return e
	}
	id := strings.TrimPrefix(location.Fragment, "oauth/")
	if len(id) != 64 {
		return fmt.Errorf("invalid OAuth consent location")
	}
	var consent mcpoauth.Consent
	if _, e = call("GET", "/v1/oauth/requests/"+id, "", nil, 200, &consent); e != nil {
		return e
	}
	if consent.ClientID != clientInfo.ID || consent.Resource != api+"/mcp" || consent.Scope != "data:read" {
		return fmt.Errorf("OAuth consent binding mismatch")
	}
	var approved struct {
		Redirect string `json:"redirect_uri"`
	}
	if _, e = call("POST", "/v1/oauth/requests/"+id, "application/json", []byte(`{"approve":true}`), 200, &approved); e != nil {
		return e
	}
	callback, e := url.Parse(approved.Redirect)
	if e != nil {
		return e
	}
	if callback.Query().Get("state") != params.Get("state") || callback.Query().Get("iss") != api {
		return fmt.Errorf("OAuth state/issuer mismatch")
	}
	form := url.Values{"grant_type": {"authorization_code"}, "client_id": {clientInfo.ID}, "code": {callback.Query().Get("code")}, "redirect_uri": {params.Get("redirect_uri")}, "resource": {api + "/mcp"}, "code_verifier": {verifier}}
	var tokens mcpoauth.Tokens
	if _, e = call("POST", "/oauth/token", "application/x-www-form-urlencoded", []byte(form.Encode()), 200, &tokens); e != nil {
		return e
	}
	revoke := func(raw string) error {
		_, e := call("POST", "/oauth/revoke", "application/x-www-form-urlencoded", []byte(url.Values{"client_id": {clientInfo.ID}, "token": {raw}}.Encode()), 200, nil)
		return e
	}
	defer revoke(tokens.RefreshToken)
	refresh := url.Values{"grant_type": {"refresh_token"}, "client_id": {clientInfo.ID}, "refresh_token": {tokens.RefreshToken}, "resource": {api + "/mcp"}}
	var renewed mcpoauth.Tokens
	if _, e = call("POST", "/oauth/token", "application/x-www-form-urlencoded", []byte(refresh.Encode()), 200, &renewed); e != nil {
		return e
	}
	if renewed.RefreshToken == "" || renewed.RefreshToken == tokens.RefreshToken {
		return fmt.Errorf("OAuth refresh did not rotate")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "0xbot-oauth-smoke", Version: "1"}, nil)
	session, e := mcpClient.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: api + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport{secret: renewed.AccessToken}, Timeout: 20 * time.Second}}, nil)
	if e != nil {
		return fmt.Errorf("OAuth MCP connection failed")
	}
	defer session.Close()
	for _, tool := range []string{"get_supported_tokens", "get_native_balance"} {
		args := map[string]string{}
		if tool == "get_native_balance" {
			args = map[string]string{"address": address, "network": "eip155:1", "provider": "auto"}
		}
		result, e := session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
		if e != nil || result.IsError || len(result.Content) == 0 {
			return fmt.Errorf("OAuth MCP tool verification failed")
		}
	}
	if e = revoke(renewed.RefreshToken); e != nil {
		return e
	}
	req, _ := http.NewRequest("POST", api+"/mcp", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer "+renewed.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	res, e := client.Do(req)
	if e != nil {
		return fmt.Errorf("OAuth revocation check transport failed")
	}
	defer res.Body.Close()
	if res.StatusCode != 401 {
		return fmt.Errorf("revoked OAuth token accepted")
	}
	fmt.Println("OAuth discovery-compatible flow, explicit HTTP consent, PKCE exchange, refresh rotation, real MCP queries and revocation verified.")
	return nil
}
