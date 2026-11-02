package evmrpc

import (
	"0xbot.org/api/internal/data"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTokenBalanceAndCommonSnapshot(t *testing.T) {
	for _, network := range data.Networks() {
		t.Run(network.ID, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var q struct {
					Method string
					Params []json.RawMessage
				}
				json.NewDecoder(r.Body).Decode(&q)
				value := "0x1"
				switch q.Method {
				case "eth_chainId":
					value = network.ChainHex
				case "eth_blockNumber":
					value = "0x123"
				case "eth_getBalance":
					if string(q.Params[1]) != `"0x123"` {
						t.Error("native not pinned")
					}
				case "eth_call":
					var call map[string]string
					json.Unmarshal(q.Params[0], &call)
					if string(q.Params[1]) != `"0x123"` {
						t.Error("token not pinned")
					}
					token, ok := data.FindToken(network.ID, call["to"])
					if !ok {
						t.Error("unregistered contract")
					}
					if call["data"] == "0x313ce567" {
						value = fmt.Sprintf("0x%064x", token.Decimals)
					} else {
						if call["data"] != "0x70a08231"+strings.Repeat("0", 63)+"1" {
							t.Error("invalid balanceOf encoding")
						}
						value = "0x" + strings.Repeat("f", 64)
					}
				default:
					t.Errorf("unexpected RPC %s", q.Method)
				}
				json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": value})
			}))
			defer s.Close()
			p := NewForNetwork("test", s.URL, network.ID, s.Client())
			req := data.Request{Address: "0x0000000000000000000000000000000000000001", Network: network.ID}
			max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1)).String()
			for _, token := range data.Tokens() {
				if token.Network != network.ID {
					continue
				}
				req.Contract = token.Contract
				b, err := p.Balance(context.Background(), req)
				if err != nil || b.Amount != max || b.Decimals != token.Decimals || b.Contract != token.Contract || b.Variant != token.Variant || b.Asset != network.ID+"/erc20:"+token.Contract {
					t.Fatalf("incorrect token result %+v %v", b, err)
				}
			}
			req.Contract = ""
			items, err := p.Balances(context.Background(), req)
			if err != nil || len(items) != 3 {
				t.Fatalf("snapshot %v %v", items, err)
			}
			for _, b := range items {
				if b.BlockNumber != "291" || b.Network != network.ID {
					t.Fatal("mixed snapshot")
				}
			}
		})
	}
}

func TestTokenFailuresNeverBecomeZeroOrPartialSuccess(t *testing.T) {
	for _, value := range []string{"0x", "0x0", "0x" + strings.Repeat("0", 62), "0x" + strings.Repeat("0", 64) + "00", "0x" + strings.Repeat("g", 64), fmt.Sprintf("0x%064x", 18)} {
		t.Run(value, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var q struct{ Method string }
				json.NewDecoder(r.Body).Decode(&q)
				out := "0x1"
				if q.Method == "eth_call" {
					out = value
				}
				json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": out})
			}))
			defer s.Close()
			p := New("test", s.URL, s.Client())
			items, err := p.Balances(context.Background(), data.Request{Address: "0x0000000000000000000000000000000000000001", Network: "eip155:1"})
			if err == nil || len(items) != 0 {
				t.Fatal("partial or malformed response accepted")
			}
		})
	}
}
