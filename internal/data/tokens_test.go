package data

import "testing"

func TestTokenRegistry(t *testing.T) {
	seen := map[string]bool{}
	for _, token := range Tokens() {
		if !ValidAddress(token.Contract) || seen[token.Network+token.Contract] {
			t.Fatal("invalid registry")
		}
		seen[token.Network+token.Contract] = true
		in := TokenRequest{Address: "0x0000000000000000000000000000000000000001", Network: token.Network, Contract: token.Contract}
		if _, err := in.Request(); err != nil {
			t.Fatal(err)
		}
		in.Network = "eip155:999"
		if _, err := in.Request(); err == nil {
			t.Fatal("cross-network token accepted")
		}
	}
	for _, contract := range []string{"USDT", "0x0000000000000000000000000000000000000000", "0x2791bca1f2de4661ed88a30c99a7a9449aa84174"} {
		if _, err := (TokenRequest{Address: "0x0000000000000000000000000000000000000001", Network: "eip155:137", Contract: contract}).Request(); err == nil {
			t.Fatal("unregistered asset or Polygon USDC.e silently accepted")
		}
	}
}
