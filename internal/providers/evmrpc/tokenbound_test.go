package evmrpc

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/tokenbound"
)

func sampleBinding(network string) tokenbound.ResolveRequest {
	return tokenbound.ResolveRequest{Network: network, TokenContract: "0xcfcfcfcfcfcfcfcfcfcfcfcfcfcfcfcfcfcfcfcf", TokenID: "123", Implementation: "0xbebebebebebebebebebebebebebebebebebebebe", Salt: "0x" + strings.Repeat("0", 64)}
}

func TestCanonicalBytecodeVector(t *testing.T) {
	// Exact example runtime from https://eips.ethereum.org/EIPS/eip-6551#registry.
	const want = "363d3d373d3d3d363d73bebebebebebebebebebebebebebebebebebebebe5af43d82803e903d91602b57fd5bf300000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000001000000000000000000000000cfcfcfcfcfcfcfcfcfcfcfcfcfcfcfcfcfcfcfcf000000000000000000000000000000000000000000000000000000000000007b"
	r := sampleBinding("eip155:1")
	if hex.EncodeToString(accountCode(r)) != want {
		t.Fatal("runtime differs from ERC-6551 example")
	}
	// Independently checked via account(address,bytes32,uint256,address,uint256)
	// on the canonical Ethereum registry, 2026-09-07. No deployment or funds.
	if accountAddress(r) != "0x3b81c21f0be3b5380b62d6ddb02246dff803df5a" {
		t.Fatal("CREATE2 derivation differs from canonical registry result")
	}
	for _, network := range data.Networks() {
		r := sampleBinding(network.ID)
		decoded, err := decodeAccount(accountCode(r), network.ID)
		if err != nil || decoded.TokenContract != r.TokenContract || decoded.TokenID != r.TokenID || decoded.Implementation != r.Implementation || decoded.Salt != r.Salt {
			t.Fatalf("round trip: %+v %v", decoded, err)
		}
		address := accountAddress(r)
		r.Salt = "0x" + strings.Repeat("f", 64)
		if accountAddress(r) == address {
			t.Fatal("salt ignored")
		}
		r.TokenID = "115792089237316195423570985008687907853269984665640564039457584007913129639935"
		decoded, err = decodeAccount(accountCode(r), network.ID)
		if err != nil || decoded.TokenID != r.TokenID {
			t.Fatal("uint256 precision lost")
		}
	}
}

func TestTokenBoundSnapshots(t *testing.T) {
	for _, network := range data.Networks() {
		for _, deployed := range []bool{false, true} {
			t.Run(network.ID+map[bool]string{false: "/counterfactual", true: "/deployed"}[deployed], func(t *testing.T) {
				in := sampleBinding(network.ID)
				address := accountAddress(in)
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var q struct {
						Method string
						Params []string
					}
					if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
						t.Error(err)
					}
					value := ""
					switch q.Method {
					case "eth_chainId":
						value = network.ChainHex
					case "eth_blockNumber":
						value = "0x123"
					case "eth_getCode":
						if len(q.Params) != 2 || q.Params[1] != "0x123" {
							t.Error("code not pinned to snapshot block")
						}
						value = "0x"
						if q.Params[0] == in.Implementation {
							value = "0x6000"
						} else if q.Params[0] == address && deployed {
							value = "0x" + hex.EncodeToString(accountCode(in))
						} else if q.Params[0] != address {
							t.Error("unexpected address")
						}
					default:
						t.Errorf("unexpected RPC (must be read-only): %s", q.Method)
					}
					json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": value})
				}))
				defer upstream.Close()
				p := NewForNetwork("fixture", upstream.URL, network.ID, upstream.Client())
				result, err := p.ResolveTokenBoundAccount(context.Background(), in)
				if err != nil || result.Address != address || result.Deployed != deployed || !result.ImplementationDeployed || result.BlockNumber != "291" || result.ObservedAt.IsZero() {
					t.Fatalf("snapshot: %+v %v", result, err)
				}
				got, err := p.GetTokenBoundAccount(context.Background(), tokenbound.GetRequest{Address: address, Network: network.ID})
				if !deployed {
					if !errors.Is(err, tokenbound.ErrNotAccount) {
						t.Fatal("undeployed get accepted")
					}
				} else if err != nil || got.Verification != "canonical_proxy" || got.TokenID != "123" {
					t.Fatalf("get: %+v %v", got, err)
				}
			})
		}
	}
}

func TestTokenBoundFailures(t *testing.T) {
	in := sampleBinding("eip155:1")
	for _, scenario := range []string{"wrong-chain", "rpc-error", "malformed-code", "wrong-proxy", "missing-code", "wrong-address", "cross-chain", "bad-padding", "missing-implementation", "implementation-error"} {
		t.Run(scenario, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var q struct {
					Method string
					Params []string
				}
				json.NewDecoder(r.Body).Decode(&q)
				value := "0x1"
				if q.Method == "eth_chainId" && scenario == "wrong-chain" {
					value = "0x38"
				}
				if q.Method == "eth_getCode" {
					code := accountCode(in)
					if scenario == "wrong-proxy" {
						code[0] = 0
					}
					if scenario == "cross-chain" {
						code = accountCode(sampleBinding("eip155:56"))
					}
					if scenario == "bad-padding" {
						code[109] = 1
					}
					value = "0x" + hex.EncodeToString(code)
					if scenario == "missing-code" {
						value = "0x"
					}
					if scenario == "malformed-code" {
						value = "0x0"
					}
					if q.Params[0] == in.Implementation {
						value = "0x6000"
						if scenario == "missing-implementation" {
							value = "0x"
						}
						if scenario == "implementation-error" {
							w.WriteHeader(503)
							return
						}
					}
				}
				if scenario == "rpc-error" {
					json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "error": map[string]any{"code": -32000, "message": "failure"}})
					return
				}
				json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": value})
			}))
			defer upstream.Close()
			p := NewForNetwork("fixture", upstream.URL, in.Network, upstream.Client())
			address := accountAddress(in)
			if scenario == "wrong-address" {
				address = in.TokenContract
			}
			result, err := p.GetTokenBoundAccount(context.Background(), tokenbound.GetRequest{Address: address, Network: in.Network})
			if scenario == "missing-implementation" {
				if err != nil || result.ImplementationDeployed {
					t.Fatal("missing implementation misrepresented")
				}
				return
			}
			want := tokenbound.ErrNotAccount
			if scenario == "wrong-chain" || scenario == "rpc-error" || scenario == "malformed-code" || scenario == "implementation-error" {
				want = data.ErrUnavailable
			}
			if !errors.Is(err, want) || result.Address != "" {
				t.Fatalf("failure/partial success: %+v %v; want %v", result, err, want)
			}
		})
	}
}

func TestTokenBoundCancellation(t *testing.T) {
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer upstream.Close()
	defer close(release)
	p := New("fixture", upstream.URL, upstream.Client())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := p.ResolveTokenBoundAccount(ctx, sampleBinding("eip155:1")); err == nil {
		t.Fatal("canceled query succeeded")
	}
}
