package prices

import (
	"strings"
	"testing"
)

func TestInputs(t *testing.T) {
	for _, s := range []string{"BTC", "eth", " BNB "} {
		in := AssetRequest{Asset: s}
		if in.Validate() != nil || in.Provider != "auto" {
			t.Fatal(s)
		}
	}
	for _, s := range []string{"WBTC", "bitcoin", "USDT", ""} {
		in := AssetRequest{Asset: s}
		if in.Validate() == nil {
			t.Fatal(s)
		}
	}
	for _, in := range []TokenRequest{{Network: "eip155:1", Contract: "USDC"}, {Network: "eip155:1", Contract: "0x" + strings.Repeat("0", 40)}, {Network: "eip155:999", Contract: "0x" + strings.Repeat("a", 40)}} {
		if in.Validate() == nil {
			t.Fatal(in)
		}
	}
}
