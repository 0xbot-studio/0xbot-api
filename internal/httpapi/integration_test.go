package httpapi

import (
	"0xbot.org/api/internal/apikeys"
	"0xbot.org/api/internal/config"
	"0xbot.org/api/internal/contract"
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/identity"
	"0xbot.org/api/internal/mcpserver"
	"0xbot.org/api/internal/operations"
	"0xbot.org/api/internal/platform/postgres"
	"0xbot.org/api/internal/platform/token"
	"0xbot.org/api/internal/providers/evmrpc"
	"0xbot.org/api/internal/routing"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/ethereum/go-ethereum/crypto"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestPlatformIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to a dedicated database ending in _test")
	}
	ctx := context.Background()
	db, e := postgres.Open(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var name string
	if e = db.QueryRow(ctx, "SELECT current_database()").Scan(&name); e != nil || !strings.HasSuffix(name, "_test") {
		t.Fatal("dedicated _test database required")
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&q)
		v := "0x1"
		if q.Method == "eth_blockNumber" {
			v = "0x100"
		}
		if q.Method == "eth_getBalance" {
			v = "0xde0b6b3a7640001"
		}
		if q.Method == "eth_call" {
			v = fmt.Sprintf("0x%064x", 6)
			var input map[string]string
			json.Unmarshal(q.Params[0], &input)
			if input["to"] == "0x"+strings.Repeat("b", 40) {
				v = fmt.Sprintf("0x%064x", 0)
				if strings.HasPrefix(input["data"], "0x01ffc9a7") {
					id := input["data"][10:18]
					if id == "01ffc9a7" || id == "80ac58cd" {
						v = fmt.Sprintf("0x%064x", 1)
					}
				} else if strings.HasPrefix(input["data"], "0x6352211e") {
					v = "0x" + strings.Repeat("0", 24) + strings.Repeat("a", 40)
				}
			}
		}
		if q.Method == "eth_getCode" {
			v = "0x"
			var address string
			json.Unmarshal(q.Params[0], &address)
			if address == "0x"+strings.Repeat("b", 40) {
				v = "0x6000"
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": v})
	}))
	defer upstream.Close()
	settings := routing.Store{DB: db}
	records := operations.Store{DB: db}
	router := routing.New([]data.Provider{evmrpc.New("publicnode", upstream.URL, upstream.Client()), evmrpc.New("drpc", upstream.URL, upstream.Client()), &priceFixture{}, &swapFixture{}}, settings)
	service := operations.New(router, records)
	a := &API{Config: config.Config{AppOrigin: "http://app.test", AdminOrigin: "http://admin.test", APIOrigin: "http://api.test", AdminWallets: map[string]bool{}}, DB: db, Identity: identity.Store{DB: db}, Keys: apikeys.Store{DB: db}, Operations: records, Settings: settings, Router: router, Service: service, Spec: contract.Spec}
	a.MCP = mcpserver.New(service, func(ctx context.Context) string { return WorkspaceFromContext(ctx) })
	handler := a.Handler()
	remote := token.New()[:8]
	request := func(method, path, body, origin, bearer string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.RemoteAddr = "test-" + remote + ":8080"
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if bearer != "" {
			r.Header.Set("Authorization", "Bearer "+bearer)
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	login := func(origin string) *http.Cookie {
		key, e := crypto.GenerateKey()
		if e != nil {
			t.Fatal(e)
		}
		addr := crypto.PubkeyToAddress(key.PublicKey).Hex()
		a.Config.AdminWallets[strings.ToLower(addr)] = origin == a.Config.AdminOrigin
		c := request("POST", "/v1/auth/wallet/challenge", `{"address":"`+addr+`"}`, origin, "", nil)
		if c.Code != 200 {
			t.Fatalf("challenge %d %s", c.Code, c.Body.String())
		}
		var challenge struct{ ID, Message string }
		json.Unmarshal(c.Body.Bytes(), &challenge)
		hash := crypto.Keccak256([]byte(fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(challenge.Message), challenge.Message)))
		sig, _ := crypto.Sign(hash, key)
		sig[64] += 27
		body := fmt.Sprintf(`{"id":%q,"signature":%q}`, challenge.ID, "0x"+hex.EncodeToString(sig))
		v := request("POST", "/v1/auth/wallet/verify", body, origin, "", nil)
		if v.Code != 200 {
			t.Fatalf("verify %d %s", v.Code, v.Body.String())
		}
		if replay := request("POST", "/v1/auth/wallet/verify", body, origin, "", nil); replay.Code != 401 {
			t.Fatal("nonce replay accepted")
		}
		return v.Result().Cookies()[0]
	}
	user1, user2 := login(a.Config.AppOrigin), login(a.Config.AppOrigin)
	if r := request("GET", "/v1/auth/me", "", a.Config.AppOrigin, "", user1); r.Code != 200 {
		t.Fatal("session not usable")
	}
	if r := request("POST", "/v1/keys", `{"name":"csrf"}`, "", "", user1); r.Code != 403 {
		t.Fatal("cookie request without origin accepted")
	}
	if r := request("POST", "/v1/keys", `{"name":"csrf"}`, "https://evil.test", "", user1); r.Code != 403 {
		t.Fatal("cross-origin write accepted")
	}
	create := request("POST", "/v1/keys", `{"name":"integration"}`, a.Config.AppOrigin, "", user1)
	if create.Code != 200 {
		t.Fatal(create.Body.String())
	}
	var key apikeys.Created
	json.Unmarshal(create.Body.Bytes(), &key)
	list := request("GET", "/v1/keys", "", a.Config.AppOrigin, "", user2)
	if bytes.Contains(list.Body.Bytes(), []byte(key.Key.ID)) {
		t.Fatal("cross-tenant key disclosed")
	}
	if r := request("DELETE", "/v1/keys/"+key.Key.ID, "", a.Config.AppOrigin, "", user2); r.Code != 404 {
		t.Fatal("cross-tenant revoke allowed")
	}
	body := `{"address":"0x0000000000000000000000000000000000000000","network":"eip155:1","provider":"drpc"}`
	r := request("POST", "/v1/data/balance", body, "", key.Secret, nil)
	if r.Code != 200 {
		t.Fatalf("query %d %s", r.Code, r.Body.String())
	}
	var out data.Result
	json.Unmarshal(r.Body.Bytes(), &out)
	if out.Provider != "drpc" || out.Data.Amount != "1000000000000000001" {
		t.Fatal("normalization failure")
	}
	list = request("GET", "/v1/operations", "", a.Config.AppOrigin, "", user2)
	if bytes.Contains(list.Body.Bytes(), []byte(out.ID)) {
		t.Fatal("cross-tenant operation disclosed")
	}
	list = request("GET", "/v1/operations", "", a.Config.AppOrigin, "", user1)
	if !bytes.Contains(list.Body.Bytes(), []byte(out.ID)) {
		t.Fatal("operation missing")
	}
	// Same stored operation ID cannot double count usage.
	var workspace string
	db.QueryRow(ctx, "SELECT workspace_id FROM api_keys WHERE id=$1", key.Key.ID).Scan(&workspace)
	if e = records.Record(ctx, operations.Record{ID: out.ID, WorkspaceID: workspace, Capability: "data.balance", Provider: "drpc", Status: "succeeded", FeeUSD: "0", CreatedAt: out.Data.ObservedAt}); e != nil {
		t.Fatal(e)
	}
	u, e := records.Usage(ctx, workspace)
	if e != nil || u.Requests != 1 {
		t.Fatalf("duplicate metering %+v %v", u, e)
	}
	// New read capabilities share authentication, isolation and metering.
	tokenBody := `{"address":"0x0000000000000000000000000000000000000001","network":"eip155:1","contract":"0xdac17f958d2ee523a2206206994597c13d831ec7"}`
	if got := request("POST", "/v1/data/token-balance", tokenBody, a.Config.AppOrigin, "", nil); got.Code != 401 {
		t.Fatal("unauthenticated token query accepted")
	}
	tokenReply := request("POST", "/v1/data/token-balance", tokenBody, "", key.Secret, nil)
	if tokenReply.Code != 200 || !bytes.Contains(tokenReply.Body.Bytes(), []byte(`"symbol":"USDT"`)) {
		t.Fatalf("token query %d %s", tokenReply.Code, tokenReply.Body.String())
	}
	batchReply := request("POST", "/v1/data/balances", body, "", key.Secret, nil)
	var batch data.BalancesResult
	if batchReply.Code != 200 || json.Unmarshal(batchReply.Body.Bytes(), &batch) != nil || len(batch.Data) != 3 {
		t.Fatalf("batch %d %s", batchReply.Code, batchReply.Body.String())
	}
	if other := request("GET", "/v1/operations", "", a.Config.AppOrigin, "", user2); bytes.Contains(other.Body.Bytes(), []byte(batch.ID)) {
		t.Fatal("batch history leaked")
	}
	u, e = records.Usage(ctx, workspace)
	if e != nil || u.Requests != 3 {
		t.Fatal("batch should count as one operation")
	}
	if bad := request("POST", "/v1/data/token-balance", strings.Replace(tokenBody, "0xdac17f958d2ee523a2206206994597c13d831ec7", "USDT", 1), "", key.Secret, nil); bad.Code != 400 {
		t.Fatal("ticker accepted as contract")
	}
	m := request("POST", "/mcp", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`, "", key.Secret, nil)
	if m.Code != 200 || !bytes.Contains(m.Body.Bytes(), []byte("0xbot")) {
		t.Fatalf("MCP initialize %d %s", m.Code, m.Body.String())
	}
	m = request("POST", "/mcp", `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_native_balance","arguments":{"address":"0x0000000000000000000000000000000000000000","network":"eip155:1","provider":"auto"}}}`, "", key.Secret, nil)
	if m.Code != 200 || !bytes.Contains(m.Body.Bytes(), []byte("1000000000000000001")) {
		t.Fatalf("MCP query %d %s", m.Code, m.Body.String())
	}
	resolveBody := `{"network":"eip155:1","token_contract":"0xcfcfcfcfcfcfcfcfcfcfcfcfcfcfcfcfcfcfcfcf","token_id":"123","implementation":"0xbebebebebebebebebebebebebebebebebebebebe","salt":"0x` + strings.Repeat("0", 64) + `"}`
	getBody := `{"network":"eip155:1","address":"0x0000000000000000000000000000000000000001"}`
	for path, input := range map[string]string{"/v1/data/token-bound-account/resolve": resolveBody, "/v1/data/token-bound-account/get": getBody} {
		if got := request("POST", path, input, a.Config.AppOrigin, "", nil); got.Code != 401 {
			t.Fatal("unauthenticated account query accepted")
		}
	}
	resolved := request("POST", "/v1/data/token-bound-account/resolve", resolveBody, "", key.Secret, nil)
	var account struct {
		ID   string
		Data struct {
			Address      string
			Deployed     bool
			Verification string
		}
	}
	if resolved.Code != 200 || json.Unmarshal(resolved.Body.Bytes(), &account) != nil || account.Data.Address == "" || account.Data.Deployed || account.Data.Verification != "counterfactual" {
		t.Fatalf("account resolve: %d %s", resolved.Code, resolved.Body.String())
	}
	if other := request("GET", "/v1/operations", "", a.Config.AppOrigin, "", user2); bytes.Contains(other.Body.Bytes(), []byte(account.ID)) {
		t.Fatal("account history leaked")
	}
	if own := request("GET", "/v1/operations", "", a.Config.AppOrigin, "", user1); !bytes.Contains(own.Body.Bytes(), []byte(account.ID)) {
		t.Fatal("account record missing")
	}
	if got := request("POST", "/v1/data/token-bound-account/get", getBody, "", key.Secret, nil); got.Code != 422 || !bytes.Contains(got.Body.Bytes(), []byte("not_token_bound_account")) {
		t.Fatal("undeployed account misrepresented")
	}
	if got := request("POST", "/v1/data/token-bound-account/resolve", strings.Replace(resolveBody, `"123"`, `123`, 1), "", key.Secret, nil); got.Code != 400 {
		t.Fatal("numeric token ID accepted")
	}
	for tool, input := range map[string]string{"resolve_token_bound_account": resolveBody, "get_token_bound_account": getBody} {
		payload := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"` + tool + `","arguments":` + input + `}}`
		got := request("POST", "/mcp", payload, "", key.Secret, nil)
		want := "counterfactual"
		if tool == "get_token_bound_account" {
			want = "not_token_bound_account"
		}
		if got.Code != 200 || !bytes.Contains(got.Body.Bytes(), []byte(want)) {
			t.Fatalf("MCP account call: %d %s", got.Code, got.Body.String())
		}
	}
	nftInputs := map[string]string{
		"/v1/nft/token":   `{"network":"eip155:1","contract":"0x` + strings.Repeat("b", 40) + `","token_id":"0"}`,
		"/v1/nft/owner":   `{"network":"eip155:1","contract":"0x` + strings.Repeat("b", 40) + `","token_id":"0"}`,
		"/v1/nft/balance": `{"network":"eip155:1","contract":"0x` + strings.Repeat("b", 40) + `","token_id":"0","address":"0x` + strings.Repeat("a", 40) + `"}`,
	}
	for path, input := range nftInputs {
		if got := request("POST", path, input, a.Config.AppOrigin, "", nil); got.Code != 401 {
			t.Fatalf("anonymous NFT read: %d %s", got.Code, got.Body.String())
		}
		got := request("POST", path, input, "", key.Secret, nil)
		var envelope struct{ ID string }
		if got.Code != 200 || json.Unmarshal(got.Body.Bytes(), &envelope) != nil || envelope.ID == "" || !bytes.Contains(got.Body.Bytes(), []byte("erc721")) {
			t.Fatalf("NFT REST %d %s", got.Code, got.Body.String())
		}
		if other := request("GET", "/v1/operations", "", a.Config.AppOrigin, "", user2); bytes.Contains(other.Body.Bytes(), []byte(envelope.ID)) {
			t.Fatal("NFT tenant leak")
		}
		tool := map[string]string{"/v1/nft/token": "get_nft_detail", "/v1/nft/owner": "get_nft_owner", "/v1/nft/balance": "get_nft_balance"}[path]
		payload := `{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"` + tool + `","arguments":` + input + `}}`
		m := request("POST", "/mcp", payload, "", key.Secret, nil)
		if m.Code != 200 || !bytes.Contains(m.Body.Bytes(), []byte("erc721")) || bytes.Contains(m.Body.Bytes(), []byte(`"isError":true`)) {
			t.Fatalf("NFT MCP %s", m.Body.String())
		}
	}
	priceInputs := map[string]string{"/v1/prices/asset": `{"asset":"BTC"}`, "/v1/prices/token": `{"network":"eip155:1","contract":"0x0000000000000000000000000000000000000001"}`}
	for path, input := range priceInputs {
		if got := request("POST", path, input, a.Config.AppOrigin, "", nil); got.Code != 401 {
			t.Fatal("anonymous price request accepted")
		}
		got := request("POST", path, input, "", key.Secret, nil)
		var result struct{ ID string }
		if got.Code != 200 || json.Unmarshal(got.Body.Bytes(), &result) != nil || result.ID == "" || !bytes.Contains(got.Body.Bytes(), []byte("USD")) {
			t.Fatalf("price REST %d %s", got.Code, got.Body.String())
		}
		if other := request("GET", "/v1/operations", "", a.Config.AppOrigin, "", user2); bytes.Contains(other.Body.Bytes(), []byte(result.ID)) {
			t.Fatal("price tenant leak")
		}
		tool := map[string]string{"/v1/prices/asset": "get_asset_price", "/v1/prices/token": "get_token_price"}[path]
		got = request("POST", "/mcp", `{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"`+tool+`","arguments":`+input+`}}`, "", key.Secret, nil)
		if got.Code != 200 || !bytes.Contains(got.Body.Bytes(), []byte("USD")) || bytes.Contains(got.Body.Bytes(), []byte(`"isError":true`)) {
			t.Fatalf("price MCP %s", got.Body.String())
		}
	}

	swapInputs := map[string]string{
		"/v1/swaps/tokens": `{"network":"eip155:1"}`,
		"/v1/swaps/quote":  `{"from_network":"eip155:1","to_network":"eip155:8453","from_token":"0x0000000000000000000000000000000000000000","to_token":"0x0000000000000000000000000000000000000000","amount":"1000000000000000001","wallet":"0x1111111111111111111111111111111111111111","slippage_bps":50}`,
		"/v1/swaps/status": `{"from_network":"eip155:1","to_network":"eip155:8453","tx_hash":"0x` + strings.Repeat("a", 64) + `"}`,
	}
	for path, input := range swapInputs {
		if got := request("POST", path, input, a.Config.AppOrigin, "", nil); got.Code != 401 {
			t.Fatal("anonymous swap query accepted")
		}
		got := request("POST", path, input, "", key.Secret, nil)
		var envelope struct{ ID, Provider string }
		if got.Code != 200 || json.Unmarshal(got.Body.Bytes(), &envelope) != nil || envelope.ID == "" || envelope.Provider != "lifi" {
			t.Fatalf("swap REST %d %s", got.Code, got.Body.String())
		}
		if other := request("GET", "/v1/operations", "", a.Config.AppOrigin, "", user2); bytes.Contains(other.Body.Bytes(), []byte(envelope.ID)) {
			t.Fatal("swap tenant leak")
		}
		var count int
		db.QueryRow(ctx, "SELECT count(*) FROM operations WHERE id=$1", envelope.ID).Scan(&count)
		if count != 1 {
			t.Fatal("swap operation not recorded once")
		}
		tool := "get_swap_" + strings.TrimPrefix(path, "/v1/swaps/")
		m := request("POST", "/mcp", `{"jsonrpc":"2.0","id":13,"method":"tools/call","params":{"name":"`+tool+`","arguments":`+input+`}}`, "", key.Secret, nil)
		if m.Code != 200 || bytes.Contains(m.Body.Bytes(), []byte(`"isError":true`)) || !bytes.Contains(m.Body.Bytes(), []byte("lifi")) {
			t.Fatalf("swap MCP %s", m.Body.String())
		}
	}

	if r = request("DELETE", "/v1/keys/"+key.Key.ID, "", a.Config.AppOrigin, "", user1); r.Code != 200 {
		t.Fatal("revoke failed")
	}
	if r = request("POST", "/v1/data/balance", body, "", key.Secret, nil); r.Code != 401 {
		t.Fatal("revoked key accepted")
	}
	for path, input := range priceInputs {
		if got := request("POST", path, input, "", key.Secret, nil); got.Code != 401 {
			t.Fatal("revoked price key accepted")
		}
	}
	for path, input := range swapInputs {
		if got := request("POST", path, input, "", key.Secret, nil); got.Code != 401 {
			t.Fatal("revoked swap key accepted")
		}
	}
	for path, input := range nftInputs {
		if got := request("POST", path, input, "", key.Secret, nil); got.Code != 401 {
			t.Fatal("revoked NFT key accepted")
		}
	}
	for path, input := range map[string]string{"/v1/data/token-bound-account/resolve": resolveBody, "/v1/data/token-bound-account/get": getBody} {
		if got := request("POST", path, input, "", key.Secret, nil); got.Code != 401 {
			t.Fatal("revoked key accepted for account query")
		}
	}
	if r = request("GET", "/v1/admin/providers", "", a.Config.AppOrigin, "", user1); r.Code != 403 {
		t.Fatal("user has admin access")
	}
	admin := login(a.Config.AdminOrigin)
	for _, tc := range []struct {
		origin string
		cookie *http.Cookie
		status int
	}{
		{a.Config.AdminOrigin, nil, 401}, {a.Config.AppOrigin, user1, 403}, {a.Config.AdminOrigin, admin, 403},
	} {
		if got := request("GET", "/v1/admin/contracts", "", tc.origin, "", tc.cookie); got.Code != tc.status {
			t.Fatalf("contract authorization: got %d want %d", got.Code, tc.status)
		}
	}
	if r = request("GET", "/v1/admin/providers", "", a.Config.AdminOrigin, "", admin); r.Code != 403 {
		t.Fatal("admin bypassed MFA")
	}
	if r = request("GET", "/v1/auth/me", "", a.Config.AppOrigin, "", admin); r.Code != 401 {
		t.Fatal("admin session leaked to app audience")
	}
	adminSession, e := a.Identity.Session(ctx, admin.Value)
	if e != nil {
		t.Fatal(e)
	}
	// Mark only this isolated test session verified; TOTP itself has separate tests.
	if e = a.Identity.MFA(ctx, adminSession.Hash); e != nil {
		t.Fatal(e)
	}
	view := request("GET", "/v1/admin/contracts", "", a.Config.AdminOrigin, "", admin)
	if view.Code != 200 {
		t.Fatalf("verified admin contracts: %d", view.Code)
	}
	var snapshot struct {
		Capabilities []json.RawMessage
		Tools        []json.RawMessage
		Providers    []struct {
			ID           string
			Capabilities []string
		}
		Instructions string
		Schemas      map[string]json.RawMessage
	}
	if e = json.Unmarshal(view.Body.Bytes(), &snapshot); e != nil {
		t.Fatal(e)
	}
	if len(snapshot.Capabilities) != 26 || len(snapshot.Tools) != 27 || len(snapshot.Providers) != 4 || snapshot.Instructions == "" || len(snapshot.Schemas["PriceResult"]) == 0 {
		t.Fatal("incomplete contract snapshot")
	}
	for _, p := range snapshot.Providers {
		if p.ID == "lifi" {
			if len(p.Capabilities) != 3 {
				t.Fatal("swap adapter mapping")
			}
			continue
		}
		if p.ID == "defillama-prices" {
			if len(p.Capabilities) != 2 {
				t.Fatal("price adapter mapping")
			}
			continue
		}
		if len(p.Capabilities) != 8 {
			t.Fatal("adapter declarations lost")
		}
	}
	// A binding conflict is rejected, rather than merging accounts.
	id1, _ := a.Identity.FindOrCreate(ctx, "x", token.New(), "one", nil)
	subject := token.New()
	id2, _ := a.Identity.FindOrCreate(ctx, "x", subject, "two", nil)
	if id1 == id2 {
		t.Fatal("independent identities merged")
	}
	if _, e = a.Identity.FindOrCreate(ctx, "x", subject, "two", &id1); e != identity.ErrConflict {
		t.Fatal("binding collision not rejected")
	}
	if r = request("POST", "/v1/auth/logout", "", a.Config.AppOrigin, "", user1); r.Code != 200 {
		t.Fatal("logout failed")
	}
	if r = request("GET", "/v1/auth/me", "", a.Config.AppOrigin, "", user1); r.Code != 401 {
		t.Fatal("revoked session accepted")
	}
}
