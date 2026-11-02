package evmrpc

import (
	"0xbot.org/api/internal/data"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBalancePreservesPrecisionAndPinsBlock(t *testing.T) {
	seen := false
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Method string   `json:"method"`
			Params []string `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&q)
		out := "0x1"
		switch q.Method {
		case "eth_blockNumber":
			out = "0x123"
		case "eth_getBalance":
			if len(q.Params) != 2 || q.Params[1] != "0x123" {
				t.Error("balance must use observed block")
			}
			seen = true
			out = "0xde0b6b3a7640001"
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": out})
	}))
	defer s.Close()
	p := New("test", s.URL, s.Client())
	b, e := p.Balance(context.Background(), data.Request{Address: "0x0000000000000000000000000000000000000000", Network: "eip155:1"})
	if e != nil || !seen || b.Amount != "1000000000000000001" || b.BlockNumber != "291" {
		t.Fatalf("bad normalized balance: %+v %v", b, e)
	}
}
func TestRejectsInvalidQuantity(t *testing.T) {
	for _, v := range []string{"", "1", "0x", "0x01", "0x-1", "0x+1", "0xgg", "0xFFFFFFFF", "0x10000000000000000000000000000000000000000000000000000000000000000"} {
		if _, e := quantity(v); e == nil {
			t.Errorf("accepted %q", v)
		}
	}
}
func TestRejectsWrongChainAndRPCError(t *testing.T) {
	for _, payload := range []string{`{"jsonrpc":"2.0","id":1,"result":"0x89"}`, `{"jsonrpc":"2.0","id":1,"error":{"code":-32000}}`, `{"jsonrpc":"2.0","id":2,"result":"0x1"}`} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(payload)) }))
		_, e := New("test", s.URL, s.Client()).Balance(context.Background(), data.Request{Network: "eip155:1"})
		s.Close()
		if e == nil {
			t.Error("accepted wrong chain or invalid upstream response")
		}
	}
}

func TestMultichainAssetAndChainValidation(t *testing.T) {
	for _, network := range data.Networks() {
		t.Run(network.ID, func(t *testing.T) {
			wrong := false
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var q struct {
					Method string
					Params []string
				}
				json.NewDecoder(r.Body).Decode(&q)
				value := "0x123"
				if q.Method == "eth_chainId" {
					value = network.ChainHex
					if wrong {
						value = "0xffff"
					}
				}
				if q.Method == "eth_getBalance" && q.Params[1] != "0x123" {
					t.Error("unpinned balance")
				}
				json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": value})
			}))
			defer s.Close()
			p := NewForNetwork("route", s.URL, network.ID, s.Client())
			req := data.Request{Address: "0x0000000000000000000000000000000000000000", Network: network.ID}
			b, err := p.Balance(context.Background(), req)
			if err != nil || b.Network != network.ID || b.Symbol != network.Symbol || b.Asset != network.Asset || b.Decimals != 18 || b.Amount != "291" {
				t.Fatalf("wrong asset: %+v %v", b, err)
			}
			wrong = true
			if _, err := p.Balance(context.Background(), req); err == nil {
				t.Fatal("wrong chain accepted")
			}
		})
	}
}
