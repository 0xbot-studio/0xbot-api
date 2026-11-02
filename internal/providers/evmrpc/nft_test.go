package evmrpc

import (
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/nft"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func encodedNFTString(s string) string {
	return "0x" + fmt.Sprintf("%064x%064x", 32, len(s)) + hex.EncodeToString([]byte(s)) + strings.Repeat("0", (32-len(s)%32)%32*2)
}
func TestNFTString(t *testing.T) {
	for _, s := range []string{"", "hello", "中文", strings.Repeat("a", 8192)} {
		got, err := nftString(encodedNFTString(s))
		if err != nil || got != s {
			t.Fatal(err)
		}
	}
	for _, s := range []string{"0x", "0x01", encodedNFTString("x") + "00", encodedNFTString(strings.Repeat("x", 8193))} {
		if _, err := nftString(s); err == nil {
			t.Fatal("accepted invalid ABI")
		}
	}
}
func TestNFTQueries(t *testing.T) {
	for _, network := range data.Networks() {
		for _, standard := range []string{"erc721", "erc1155"} {
			t.Run(network.ID+"/"+standard, func(t *testing.T) {
				owner := "0x" + strings.Repeat("a", 40)
				revert := false
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var q struct {
						Method string
						Params []json.RawMessage
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
						value = "0x6000"
					case "eth_call":
						var in map[string]string
						json.Unmarshal(q.Params[0], &in)
						var block string
						json.Unmarshal(q.Params[1], &block)
						if block != "0x123" || in["gas"] != "0x7a120" {
							t.Error("unpinned or unbounded read")
						}
						input := in["data"]
						switch input[:10] {
						case "0x01ffc9a7":
							id := input[10:18]
							ok := id == "01ffc9a7" || id == "5b5e139f" || id == "0e89341c" || (standard == "erc721" && id == "80ac58cd") || (standard == "erc1155" && id == "d9b67a26")
							value = fmt.Sprintf("0x%064x", 0)
							if ok {
								value = fmt.Sprintf("0x%064x", 1)
							}
						case "0x6352211e":
							if revert {
								json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "error": map[string]any{"code": 3, "message": "execution reverted"}})
								return
							}
							value = "0x" + strings.Repeat("0", 24) + owner[2:]
						case "0x00fdd58e":
							value = "0x" + strings.Repeat("f", 64)
						case "0x06fdde03":
							value = encodedNFTString("Collection")
						case "0x95d89b41":
							value = encodedNFTString("NFT")
						case "0xc87b56dd", "0x0e89341c":
							value = encodedNFTString("data:application/json,{}")
						default:
							t.Error(input)
						}
					default:
						t.Error(q.Method)
					}
					json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": value})
				}))
				defer upstream.Close()
				p := NewForNetwork("test", upstream.URL, network.ID, upstream.Client())
				in := nft.TokenRequest{Network: network.ID, Contract: "0x" + strings.Repeat("b", 40), TokenID: "0"}
				detail, err := p.NFTDetail(context.Background(), in)
				if err != nil || detail.Standard != standard || detail.BlockNumber != "291" || detail.Metadata.Status != "available" || detail.ExistenceVerified != (standard == "erc721") {
					t.Fatalf("%+v %v", detail, err)
				}
				balance, err := p.NFTBalance(context.Background(), nft.BalanceRequest{Network: network.ID, Contract: in.Contract, TokenID: "0", Address: owner})
				if err != nil {
					t.Fatal(err)
				}
				want := "1"
				if standard == "erc1155" {
					want = "115792089237316195423570985008687907853269984665640564039457584007913129639935"
				}
				if balance.Amount != want {
					t.Fatal(balance)
				}
				got, err := p.NFTOwner(context.Background(), in)
				if standard == "erc1155" {
					if !errors.Is(err, nft.ErrUnsupported) {
						t.Fatal(err)
					}
				} else {
					if err != nil || got.Owner != owner {
						t.Fatal(got, err)
					}
					revert = true
					if _, err = p.NFTOwner(context.Background(), in); !errors.Is(err, nft.ErrReverted) {
						t.Fatal("revert became success", err)
					}
				}
			})
		}
	}
}
