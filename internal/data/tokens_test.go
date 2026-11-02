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

func TestEquityRegistrySeparation(t *testing.T) {
	stablecoins, equities := 0, 0
	for _, token := range Tokens() {
		switch token.AssetClass {
		case ClassStablecoin:
			stablecoins++
		case ClassTokenizedEquity:
			equities++
			if token.Network != "eip155:1" || token.Underlying == "" || token.Issuer == "" {
				t.Fatalf("incomplete equity entry: %+v", token)
			}
			in := EquityTokenRequest{Address: "0x0000000000000000000000000000000000000001", Network: token.Network, Contract: token.Contract}
			if _, err := in.Request(); err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatalf("unknown asset class: %+v", token)
		}
	}
	if stablecoins != 6 || equities != 3 {
		t.Fatalf("unexpected registry composition: %d stablecoins, %d equities", stablecoins, equities)
	}
	if _, err := (EquityTokenRequest{Address: "0x0000000000000000000000000000000000000001", Network: "eip155:1", Contract: "0xdac17f958d2ee523a2206206994597c13d831ec7"}).Request(); err == nil {
		t.Fatal("stablecoin accepted by the equity capability")
	}
	if _, err := (EquityTokenRequest{Address: "0x0000000000000000000000000000000000000001", Network: "eip155:1", Contract: "0x0000000000000000000000000000000000000000"}).Request(); err == nil {
		t.Fatal("unregistered contract accepted by the equity capability")
	}
}
