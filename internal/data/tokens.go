package data

import "strings"

// Curated contracts only. See docs/adr/0004-token-balances.md for provenance.
type Token struct {
	Network  string `json:"network"`
	Contract string `json:"contract"`
	Symbol   string `json:"symbol"`
	Name     string `json:"name"`
	Variant  string `json:"variant"`
	Decimals int    `json:"decimals"`
}

func Tokens() []Token {
	return []Token{
		{"eip155:1", "0xdac17f958d2ee523a2206206994597c13d831ec7", "USDT", "Tether USD", "issuer-native", 6},
		{"eip155:1", "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48", "USDC", "USD Coin", "issuer-native", 6},
		{"eip155:56", "0x55d398326f99059ff775485246999027b3197955", "USDT", "Binance-Peg BSC-USD", "binance-peg", 18},
		{"eip155:56", "0x8ac76a51cc950d9822d68b83fe1ad97b32cd580d", "USDC", "Binance-Peg USD Coin", "binance-peg", 18},
		{"eip155:137", "0xc2132d05d31c914a87c6611c10748aeb04b58e8f", "USDT0", "USDT0 (Polygon USDT)", "usdt0", 6},
		{"eip155:137", "0x3c499c542cef5e3811e1192ce70d8cc03d5c3359", "USDC", "USD Coin", "issuer-native", 6},
	}
}

func FindToken(network, contract string) (Token, bool) {
	for _, token := range Tokens() {
		if token.Network == network && strings.EqualFold(token.Contract, contract) {
			return token, true
		}
	}
	return Token{}, false
}

type TokenRequest struct {
	Address  string `json:"address"`
	Network  string `json:"network"`
	Provider string `json:"provider,omitempty"`
	Contract string `json:"contract" jsonschema:"Exact allowlisted token contract from the get_supported_tokens tool or GET /v1/tokens; symbols are not accepted"`
}

func (t TokenRequest) Request() (Request, error) {
	r := Request{Address: t.Address, Network: t.Network, Provider: t.Provider, Contract: strings.ToLower(t.Contract)}
	if _, ok := FindToken(r.Network, r.Contract); !ok {
		return r, ErrInput
	}
	return r, r.Validate()
}

type BalancesResult = QueryResult[[]Balance]
