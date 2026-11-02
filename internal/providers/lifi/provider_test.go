package lifi

import (
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/swaps"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

const wallet = "0x1111111111111111111111111111111111111111"
const contract = "0x2222222222222222222222222222222222222222"

func input() swaps.QuoteInput {
	return swaps.QuoteInput{FromNetwork: "eip155:1", ToNetwork: "eip155:8453", FromToken: swaps.Native, ToToken: swaps.Native, Amount: "900719925474099300001", Wallet: wallet, SlippageBPS: 50}
}
func fixture() map[string]any {
	return map[string]any{
		"id": "fixture-quote", "tool": "fixture-bridge",
		"action":             map[string]any{"fromChainId": 1, "toChainId": 8453, "fromToken": map[string]any{"address": swaps.Native, "chainId": 1, "symbol": "ETH", "name": "Ether", "decimals": 18}, "toToken": map[string]any{"address": swaps.Native, "chainId": 8453, "symbol": "ETH", "name": "Ether", "decimals": 18}, "fromAmount": input().Amount, "fromAddress": wallet, "toAddress": wallet, "slippage": json.Number("0.005")},
		"estimate":           map[string]any{"fromAmount": input().Amount, "toAmount": "900719925474099300000", "toAmountMin": "890719925474099300000", "approvalAddress": contract, "feeCosts": []any{}, "gasCosts": []any{}},
		"transactionRequest": map[string]any{"from": wallet, "to": contract, "chainId": 1, "value": input().Amount, "data": "0x12345678"},
	}
}
func TestQuoteContract(t *testing.T) {
	body := fixture()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/quote" || r.URL.Query().Get("slippage") != "0.0050" || r.URL.Query().Get("fromAmount") != input().Amount || r.Header.Get("x-lifi-api-key") != "fixture-secret" {
			t.Error("incorrect boundary")
		}
		json.NewEncoder(w).Encode(body)
	}))
	defer server.Close()
	p := New("fixture-secret", "0xbot")
	p.base = server.URL
	out, e := p.Quote(context.Background(), input())
	if e != nil || out.Transaction.Value != input().Amount || out.Request.Amount != input().Amount || out.ExpiresAt.Sub(out.FetchedAt) != time.Minute {
		t.Fatal(out, e)
	}
	mutations := []func(map[string]any){
		func(m map[string]any) { delete(m["action"].(map[string]any)["fromToken"].(map[string]any), "decimals") },
		func(m map[string]any) { m["action"].(map[string]any)["toAddress"] = contract },
		func(m map[string]any) { m["action"].(map[string]any)["fromAmount"] = "1" },
		func(m map[string]any) { m["action"].(map[string]any)["slippage"] = json.Number("0.01") },
		func(m map[string]any) { m["action"].(map[string]any)["toToken"].(map[string]any)["address"] = contract },
		func(m map[string]any) { m["transactionRequest"].(map[string]any)["chainId"] = 56 },
		func(m map[string]any) { m["transactionRequest"].(map[string]any)["value"] = "900719925474099300002" },
		func(m map[string]any) { m["transactionRequest"].(map[string]any)["from"] = contract },
		func(m map[string]any) { m["transactionRequest"].(map[string]any)["data"] = "0x123" },
		func(m map[string]any) { m["estimate"].(map[string]any)["toAmountMin"] = "900719925474099300001" },
		func(m map[string]any) { m["estimate"].(map[string]any)["toAmount"] = "0" },
	}
	for i, mutate := range mutations {
		body = fixture()
		mutate(body)
		if _, e := p.Quote(context.Background(), input()); e != data.ErrUnavailable {
			t.Fatalf("mutation %d: %v", i, e)
		}
	}
	if calls != 1+len(mutations) {
		t.Fatal("unexpected retry")
	}
}
func TestStatusNeverInventsCompletion(t *testing.T) {
	hash := "0x" + strings.Repeat("a", 64)
	var state, sub string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"status": state, "substatus": sub, "sending": map[string]any{"txHash": hash, "chainId": 1}, "receiving": map[string]any{"txHash": hash, "chainId": 8453}})
	}))
	defer server.Close()
	p := New("fixture", "0xbot")
	p.base = server.URL
	for _, c := range []struct{ s, sub, want string }{{"NOT_FOUND", "", "unknown"}, {"DONE", "REFUNDED", "unknown"}, {"DONE", "PARTIAL", "unknown"}, {"DONE", "COMPLETED", "completed"}, {"PENDING", "", "pending"}, {"FAILED", "", "failed"}} {
		state, sub = c.s, c.sub
		out, e := p.Status(context.Background(), swaps.StatusInput{FromNetwork: "eip155:1", ToNetwork: "eip155:8453", TxHash: hash})
		if e != nil || out.State != c.want {
			t.Fatal(out, e)
		}
	}
}
func TestCatalogCachingAndLimits(t *testing.T) {
	calls := 0
	rateLimited := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if rateLimited {
			w.WriteHeader(429)
			return
		}
		w.Write([]byte(`{"tokens":{"1":[{"chainId":1,"address":"` + swaps.Native + `","symbol":"ETH","name":"Ether","decimals":18},{"chainId":56,"address":"` + swaps.Native + `","symbol":"BNB","decimals":18}]}}`))
	}))
	defer server.Close()
	p := New("fixture", "0xbot")
	p.base = server.URL
	for i := 0; i < 2; i++ {
		v, e := p.Tokens(context.Background(), swaps.TokensInput{Network: "eip155:1"})
		if e != nil || len(v) != 1 {
			t.Fatal(v, e)
		}
		v[0].Symbol = "MUTATED"
	}
	if calls != 1 {
		t.Fatal("not cached")
	}
	rateLimited = true
	_, e := p.Tokens(context.Background(), swaps.TokensInput{Network: "eip155:56"})
	if e != data.ErrRateLimited {
		t.Fatal(e)
	}
	_, e = p.Tokens(context.Background(), swaps.TokensInput{Network: "eip155:56"})
	if e != data.ErrRateLimited || calls != 2 {
		t.Fatal("missing cooldown", e, calls)
	}
}
func TestMissingConfigAndCancellation(t *testing.T) {
	p := New("", "0xbot")
	if _, e := p.Quote(context.Background(), input()); e != data.ErrUnavailable {
		t.Fatal(e)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	p = New("fixture", "0xbot")
	p.base = server.URL
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, e := p.Quote(ctx, input()); e != data.ErrUnavailable {
		t.Fatal(e)
	}
}

// Public quote captured 2026-09-21; no wallet submission or live upstream in tests.
func TestRecordedNativeBridge(t *testing.T) {
	b, e := os.ReadFile("testdata/native-bridge.json")
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(b) }))
	defer server.Close()
	p := New("fixture", "0xbot")
	p.base = server.URL
	in := input()
	in.Amount = "10000000000000000"
	q, e := p.Quote(context.Background(), in)
	if e != nil || q.Transaction.Value != in.Amount || len(q.Costs) != 4 || q.MinimumAmount != "9972127103764960" {
		t.Fatal(q, e)
	}
}

func TestExplicitPublicAccessQuoteQuotaAndStatus(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("x-lifi-api-key") != "" {
			t.Error("unexpected public credential")
		}
		w.Write([]byte(`{}`))
	}))
	defer server.Close()
	p := NewPublic("0xbot")
	p.base = server.URL
	for i := 0; i < 70; i++ {
		if e := p.get(context.Background(), "/quote", nil, &map[string]any{}); e != nil {
			t.Fatal(e)
		}
	}
	if e := p.get(context.Background(), "/quote", nil, &map[string]any{}); e != data.ErrRateLimited || calls != 70 {
		t.Fatal("public quota failed", e)
	}
	if e := p.get(context.Background(), "/status", nil, &map[string]any{}); e != nil {
		t.Fatal("quote quota blocked receipts", e)
	}
	p.quoteTimes[0] = time.Now().Add(-2*time.Hour - time.Second)
	if e := p.get(context.Background(), "/quote", nil, &map[string]any{}); e != nil {
		t.Fatal("rolling quota did not release", e)
	}
}
func TestQuoteRateLimitHonorsResetWithoutBlockingStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/quote" {
			w.Header().Set("ratelimit-reset", "3600")
			w.WriteHeader(429)
			return
		}
		w.Write([]byte(`{}`))
	}))
	defer server.Close()
	p := NewPublic("0xbot")
	p.base = server.URL
	if e := p.get(context.Background(), "/quote", nil, &map[string]any{}); e != data.ErrRateLimited {
		t.Fatal(e)
	}
	if time.Until(p.quoteCooldown) < 59*time.Minute {
		t.Fatal("upstream reset discarded")
	}
	if e := p.get(context.Background(), "/status", nil, &map[string]any{}); e != nil {
		t.Fatal(e)
	}
}
