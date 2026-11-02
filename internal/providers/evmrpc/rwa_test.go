package evmrpc

import (
	"0xbot.org/api/internal/data"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// word left-pads a hex value into a 32-byte ABI word exactly 66 chars long.
func word(hexValue string) string {
	if len(hexValue) > 64 {
		panic("word too long")
	}
	return "0x" + strings.Repeat("0", 64-len(hexValue)) + hexValue
}

func equityFixture(t *testing.T) data.Token {
	t.Helper()
	for _, token := range data.Tokens() {
		if token.AssetClass == data.ClassTokenizedEquity {
			return token
		}
	}
	t.Fatal("no curated equity token")
	return data.Token{}
}

func TestRWATokenBalancePinsBlockAndVerifiesDecimals(t *testing.T) {
	token := equityFixture(t)
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Method string
			Params []json.RawMessage
		}
		json.NewDecoder(r.Body).Decode(&q)
		out := "0x1"
		switch q.Method {
		case "eth_chainId":
			out = "0x1"
		case "eth_blockNumber":
			out = "0x123"
		case "eth_call":
			calls++
			var call map[string]string
			json.Unmarshal(q.Params[0], &call)
			if call["to"] != token.Contract {
				t.Errorf("unexpected call target %s", call["to"])
			}
			if string(q.Params[1]) != `"0x123"` {
				t.Error("equity read not pinned to observed block")
			}
			if strings.HasPrefix(call["data"], "0x313ce567") {
				out = word("12") // 18 decimals
			} else {
				out = word("de0b6b3a7640001")
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": out})
	}))
	defer s.Close()
	p := NewForNetwork("test", s.URL, "eip155:1", s.Client())
	in := data.EquityTokenRequest{Address: "0x0000000000000000000000000000000000000001", Network: "eip155:1", Contract: token.Contract}
	req, err := in.Request()
	if err != nil {
		t.Fatal(err)
	}
	b, e := p.RWATokenBalance(context.Background(), req)
	if e != nil || calls != 2 || b.Amount != "1000000000000000001" || b.BlockNumber != "291" || b.Variant != token.Variant || b.Symbol != token.Symbol {
		t.Fatalf("bad equity balance: %+v calls=%d err=%v", b, calls, e)
	}
	if b.ObservedAt.IsZero() || b.ObservedAt.After(time.Now().UTC().Add(time.Minute)) {
		t.Fatal("bad observation time")
	}
}

func TestRWATokenBalanceRejectsStablecoinContract(t *testing.T) {
	p := NewForNetwork("test", "http://unused.invalid", "eip155:1", nil)
	req := data.Request{Address: "0x0000000000000000000000000000000000000001", Network: "eip155:1", Contract: "0xdac17f958d2ee523a2206206994597c13d831ec7"}
	if _, e := p.RWATokenBalance(context.Background(), req); e != data.ErrInput {
		t.Fatal("stablecoin accepted by the equity capability")
	}
}

// The common-balance snapshot must stay a fixed three-asset contract: curated
// tokenized-equity entries must not leak into it.
func TestCommonBalancesExcludeEquityEntries(t *testing.T) {
	equitySeen := false
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Method string
			Params []json.RawMessage
		}
		json.NewDecoder(r.Body).Decode(&q)
		out := "0x1"
		switch q.Method {
		case "eth_chainId":
			out = "0x1"
		case "eth_blockNumber":
			out = "0x123"
		case "eth_call":
			var call map[string]string
			json.Unmarshal(q.Params[0], &call)
			target, ok := data.FindToken("eip155:1", call["to"])
			if !ok {
				t.Errorf("eth_call to unknown contract %s", call["to"])
			}
			if target.AssetClass == data.ClassTokenizedEquity {
				equitySeen = true
			}
			if strings.HasPrefix(call["data"], "0x313ce567") {
				decWord := fmt.Sprintf("%x", target.Decimals)
				out = "0x" + strings.Repeat("0", 64-len(decWord)) + decWord
			} else {
				out = word("de0b6b3a7640001")
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": out})
	}))
	defer s.Close()
	p := NewForNetwork("test", s.URL, "eip155:1", s.Client())
	items, e := p.Balances(context.Background(), data.Request{Address: "0x0000000000000000000000000000000000000001", Network: "eip155:1"})
	if e != nil || len(items) != 3 || equitySeen {
		t.Fatalf("common snapshot must stay three assets: %d items, equitySeen=%v err=%v", len(items), equitySeen, e)
	}
}
