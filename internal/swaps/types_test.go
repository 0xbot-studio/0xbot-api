package swaps

import (
	"math/big"
	"strings"
	"testing"
)

func TestQuoteValidation(t *testing.T) {
	valid := QuoteInput{FromNetwork: "eip155:1", ToNetwork: "eip155:8453", FromToken: Native, ToToken: Native, Amount: "900719925474099300001", Wallet: "0x" + strings.Repeat("A", 40), SlippageBPS: 50}
	if e := valid.Validate(); e != nil || strings.Contains(valid.Wallet, "A") {
		t.Fatal(e)
	}
	for _, change := range []func(*QuoteInput){func(r *QuoteInput) { r.Amount = "0" }, func(r *QuoteInput) { r.Amount = "1e18" }, func(r *QuoteInput) { r.Amount = "01" }, func(r *QuoteInput) { r.Amount = new(big.Int).Lsh(big.NewInt(1), 256).String() }, func(r *QuoteInput) { r.Wallet = Native }, func(r *QuoteInput) { r.ToToken = "USDC" }, func(r *QuoteInput) { r.ToNetwork = "solana" }, func(r *QuoteInput) { r.ToNetwork = r.FromNetwork }, func(r *QuoteInput) { r.SlippageBPS = 101 }} {
		in := valid
		change(&in)
		if in.Validate() == nil {
			t.Fatal("accepted invalid request", in)
		}
	}
}
