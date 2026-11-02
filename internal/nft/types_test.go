package nft

import (
	"strings"
	"testing"
)

func TestTokenIDs(t *testing.T) {
	for _, id := range []string{"0", "1", "115792089237316195423570985008687907853269984665640564039457584007913129639935"} {
		r := TokenRequest{Network: "eip155:56", Contract: "0x" + strings.Repeat("A", 40), TokenID: id}
		if r.Validate() != nil || r.Contract != "0x"+strings.Repeat("a", 40) {
			t.Fatal(id)
		}
	}
	for _, id := range []string{"", "-1", "01", "1.0", "1e3", "0x1", "115792089237316195423570985008687907853269984665640564039457584007913129639936"} {
		r := TokenRequest{Network: "eip155:1", Contract: "0x" + strings.Repeat("a", 40), TokenID: id}
		if r.Validate() == nil {
			t.Fatal(id)
		}
	}
	r := BalanceRequest{Network: "eip155:1", Contract: "0x" + strings.Repeat("a", 40), TokenID: "0", Address: "0x" + strings.Repeat("0", 40)}
	if r.Validate() == nil {
		t.Fatal("zero holder")
	}
}
