package tokenbound

import (
	"math/big"
	"strings"
	"testing"
)

func validResolve() ResolveRequest {
	return ResolveRequest{Network: "eip155:56", TokenContract: "0x" + strings.Repeat("ab", 20), TokenID: "123", Implementation: "0x" + strings.Repeat("cd", 20), Salt: "0x" + strings.Repeat("0", 64)}
}
func TestResolveValidation(t *testing.T) {
	for name, change := range map[string]func(*ResolveRequest){
		"missing salt":        func(r *ResolveRequest) { r.Salt = "" },
		"short salt":          func(r *ResolveRequest) { r.Salt = "0x00" },
		"salt hex":            func(r *ResolveRequest) { r.Salt = "0x" + strings.Repeat("g", 64) },
		"implementation":      func(r *ResolveRequest) { r.Implementation = "" },
		"zero implementation": func(r *ResolveRequest) { r.Implementation = "0x" + strings.Repeat("0", 40) },
		"zero NFT":            func(r *ResolveRequest) { r.TokenContract = "0x" + strings.Repeat("0", 40) },
		"chain":               func(r *ResolveRequest) { r.Network = "eip155:10" },
		"negative":            func(r *ResolveRequest) { r.TokenID = "-1" },
		"hex":                 func(r *ResolveRequest) { r.TokenID = "0xff" },
		"leading zero":        func(r *ResolveRequest) { r.TokenID = "01" },
		"float":               func(r *ResolveRequest) { r.TokenID = "1.5" },
		"exponent":            func(r *ResolveRequest) { r.TokenID = "1e3" },
		"overflow":            func(r *ResolveRequest) { r.TokenID = new(big.Int).Lsh(big.NewInt(1), 256).String() },
	} {
		t.Run(name, func(t *testing.T) {
			r := validResolve()
			change(&r)
			if r.Validate() == nil {
				t.Fatal("invalid request accepted")
			}
		})
	}
	for _, id := range []string{"0", new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1)).String()} {
		r := validResolve()
		r.TokenID = id
		r.Implementation = "0x" + strings.Repeat("CD", 20)
		if r.Validate() != nil || r.Provider != "auto" || r.Implementation != "0x"+strings.Repeat("cd", 20) {
			t.Fatal("valid request rejected or not normalized")
		}
	}
}
