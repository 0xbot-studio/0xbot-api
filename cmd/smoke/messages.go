package main

import (
	"0xbot.org/api/internal/apikeys"
	"0xbot.org/api/internal/mcpoauth"
	"0xbot.org/api/internal/messaging"
	"0xbot.org/api/internal/platform/token"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

type messageActor struct {
	client            *http.Client
	api, app, address string
}

func (a *messageActor) call(method, path string, body any, bearer string, want int, out any) error {
	raw, _ := json.Marshal(body)
	typ := "application/json"
	if form, ok := body.(url.Values); ok {
		raw = []byte(form.Encode())
		typ = "application/x-www-form-urlencoded"
	}
	req, e := http.NewRequest(method, a.api+path, bytes.NewReader(raw))
	if e != nil {
		return e
	}
	req.Header.Set("Origin", a.app)
	req.Header.Set("Content-Type", typ)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	res, e := a.client.Do(req)
	if e != nil {
		return fmt.Errorf("message smoke %s: transport failure", path)
	}
	defer res.Body.Close()
	if res.StatusCode != want {
		return fmt.Errorf("message smoke %s: expected %d got %d", path, want, res.StatusCode)
	}
	if out != nil {
		return json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(out)
	}
	return nil
}
func newMessageActor(api, app string) (*messageActor, error) {
	jar, _ := cookiejar.New(nil)
	a := &messageActor{api: api, app: app, client: &http.Client{Jar: jar, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	key, e := crypto.GenerateKey()
	if e != nil {
		return nil, e
	}
	a.address = strings.ToLower(crypto.PubkeyToAddress(key.PublicKey).Hex())
	var c struct{ ID, Message string }
	if e = a.call("POST", "/v1/auth/wallet/challenge", map[string]string{"address": a.address}, "", 200, &c); e != nil {
		return nil, e
	}
	sig, e := crypto.Sign(crypto.Keccak256([]byte(fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(c.Message), c.Message))), key)
	if e != nil {
		return nil, e
	}
	sig[64] += 27
	if e = a.call("POST", "/v1/auth/wallet/verify", map[string]string{"id": c.ID, "signature": "0x" + hex.EncodeToString(sig)}, "", 200, nil); e != nil {
		return nil, e
	}
	return a, nil
}
func (a *messageActor) grant(scope string) (mcpoauth.Tokens, func(), error) {
	none := func() {}
	var c mcpoauth.Client
	if e := a.call("POST", "/oauth/register", mcpoauth.Registration{Name: "PADIN deployment smoke", Redirects: []string{"http://127.0.0.1/callback"}}, "", 201, &c); e != nil {
		return mcpoauth.Tokens{}, none, e
	}
	verifier := token.New()
	q := url.Values{"client_id": {c.ID}, "redirect_uri": {"http://127.0.0.1/callback"}, "response_type": {"code"}, "resource": {a.api + "/mcp"}, "scope": {scope}, "code_challenge": {mcpoauth.Challenge(verifier)}, "code_challenge_method": {"S256"}, "state": {token.New()}}
	res, e := a.client.Get(a.api + "/oauth/authorize?" + q.Encode())
	if e != nil {
		return mcpoauth.Tokens{}, none, fmt.Errorf("message authorization transport failed")
	}
	res.Body.Close()
	if res.StatusCode != 302 {
		return mcpoauth.Tokens{}, none, fmt.Errorf("message authorization status %d", res.StatusCode)
	}
	redirect, e := url.Parse(res.Header.Get("Location"))
	if e != nil {
		return mcpoauth.Tokens{}, none, e
	}
	id := strings.TrimPrefix(redirect.Fragment, "oauth/")
	var consent mcpoauth.Consent
	if e = a.call("GET", "/v1/oauth/requests/"+id, nil, "", 200, &consent); e != nil {
		return mcpoauth.Tokens{}, none, e
	}
	if consent.Scope != scope {
		return mcpoauth.Tokens{}, none, fmt.Errorf("consent scope mismatch")
	}
	// Approve only our new synthetic account, never a real user's grant.
	var decision struct {
		Redirect string `json:"redirect_uri"`
	}
	if e = a.call("POST", "/v1/oauth/requests/"+id, map[string]any{"approve": true, "wallet": a.address}, "", 200, &decision); e != nil {
		return mcpoauth.Tokens{}, none, e
	}
	callback, e := url.Parse(decision.Redirect)
	if e != nil {
		return mcpoauth.Tokens{}, none, e
	}
	if callback.Query().Get("state") != q.Get("state") {
		return mcpoauth.Tokens{}, none, fmt.Errorf("state mismatch")
	}
	var tokens mcpoauth.Tokens
	e = a.call("POST", "/oauth/token", url.Values{"grant_type": {"authorization_code"}, "client_id": {c.ID}, "code": {callback.Query().Get("code")}, "redirect_uri": {q.Get("redirect_uri")}, "code_verifier": {verifier}, "resource": {a.api + "/mcp"}}, "", 200, &tokens)
	revoke := func() {
		if tokens.AccessToken != "" {
			if err := a.call("POST", "/oauth/revoke", url.Values{"client_id": {c.ID}, "token": {tokens.AccessToken}}, "", 200, nil); err != nil {
				fmt.Println("WARNING: smoke grant cleanup failed")
			}
		}
	}
	if e != nil {
		return tokens, revoke, e
	}
	if tokens.Scope != scope {
		return tokens, revoke, fmt.Errorf("token scope mismatch")
	}
	return tokens, revoke, nil
}
func messageSmoke(api, app string) error {
	sender, e := newMessageActor(api, app)
	if e != nil {
		return e
	}
	defer sender.call("POST", "/v1/auth/logout", nil, "", 200, nil)
	recipient, e := newMessageActor(api, app)
	if e != nil {
		return e
	}
	defer recipient.call("POST", "/v1/auth/logout", nil, "", 200, nil)
	in := messaging.SendInput{From: sender.address, To: recipient.address, Body: "PADIN Inbox deployment smoke: synthetic test wallets only.", IdempotencyKey: token.New(), Provider: messaging.ProviderID}
	var sent, retry messaging.Result
	if e = sender.call("POST", "/v1/messages/send", in, "", 200, &sent); e != nil {
		return e
	}
	if e = sender.call("POST", "/v1/messages/send", in, "", 200, &retry); e != nil {
		return e
	}
	if sent.ID == "" || retry.ID != sent.ID || sent.Data.Status != "stored" {
		return fmt.Errorf("message idempotency/status mismatch")
	}
	conflict := in
	conflict.Body = "different"
	if e = sender.call("POST", "/v1/messages/send", conflict, "", 409, nil); e != nil {
		return e
	}
	var page messaging.ListResult
	list := messaging.ListInput{Address: recipient.address, Limit: 20}
	if e = recipient.call("POST", "/v1/messages/list", list, "", 200, &page); e != nil {
		return e
	}
	if len(page.Data.Messages) != 1 || page.Data.Messages[0].ID != sent.Data.ID {
		return fmt.Errorf("inbox mismatch")
	}
	if e = sender.call("POST", "/v1/messages/list", list, "", 403, nil); e != nil {
		return e
	}
	detail := messaging.GetInput{Address: recipient.address, ID: sent.Data.ID}
	var got messaging.Result
	if e = recipient.call("POST", "/v1/messages/get", detail, "", 200, &got); e != nil {
		return e
	}
	if got.Data.Body != in.Body {
		return fmt.Errorf("message detail mismatch")
	}
	foreign := detail
	foreign.Address = sender.address
	if e = sender.call("POST", "/v1/messages/get", foreign, "", 404, nil); e != nil {
		return e
	}
	var key apikeys.Created
	if e = sender.call("POST", "/v1/keys", map[string]string{"name": "PADIN smoke read-only denial"}, "", 200, &key); e != nil {
		return e
	}
	defer sender.call("DELETE", "/v1/keys/"+key.Key.ID, nil, "", 200, nil)
	if e = sender.call("POST", "/v1/messages/send", in, key.Secret, 403, nil); e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	connect := func(secret string) (*mcp.ClientSession, error) {
		client := mcp.NewClient(&mcp.Implementation{Name: "PADIN smoke", Version: "1"}, nil)
		return client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: api + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport{secret: secret}, Timeout: 20 * time.Second}}, nil)
	}
	readTokens, revokeRead, e := recipient.grant("data:read messages:read")
	defer revokeRead()
	if e != nil {
		return e
	}
	read, e := connect(readTokens.AccessToken)
	if e != nil {
		return e
	}
	defer read.Close()
	out, e := read.CallTool(ctx, &mcp.CallToolParams{Name: "get_message", Arguments: detail})
	if e != nil || out.IsError {
		return fmt.Errorf("MCP recipient detail failed")
	}
	raw, _ := json.Marshal(out.StructuredContent)
	if !bytes.Contains(raw, []byte(in.Body)) {
		return fmt.Errorf("MCP body mismatch")
	}
	out, e = read.CallTool(ctx, &mcp.CallToolParams{Name: "send_message", Arguments: in})
	if e != nil || !out.IsError {
		return fmt.Errorf("read-only grant allowed sending")
	}
	sendTokens, revokeSend, e := sender.grant("data:read messages:send")
	defer revokeSend()
	if e != nil {
		return e
	}
	send, e := connect(sendTokens.AccessToken)
	if e != nil {
		return e
	}
	defer send.Close()
	out, e = send.CallTool(ctx, &mcp.CallToolParams{Name: "send_message", Arguments: in})
	if e != nil || out.IsError {
		return fmt.Errorf("MCP sender failed")
	}
	raw, _ = json.Marshal(out.StructuredContent)
	if !bytes.Contains(raw, []byte(sent.ID)) {
		return fmt.Errorf("REST/MCP idempotency mismatch")
	}
	out, e = read.CallTool(ctx, &mcp.CallToolParams{Name: "list_messages", Arguments: list})
	if e != nil || out.IsError {
		return fmt.Errorf("MCP inbox failed")
	}
	fmt.Println("PADIN Inbox REST/MCP send/list/detail, wallet-scoped OAuth, idempotency, recipient isolation and read-only denial verified. Test credentials are revoked on exit; synthetic message/history retained.")
	return nil
}
