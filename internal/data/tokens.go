package data

import "strings"

// Curated contracts only. See docs/adr/0004-token-balances.md for provenance and
// docs/adr/0015-rwa-equity-reads.md for the tokenized-equity entries.
type Token struct {
	Network    string `json:"network"`
	Contract   string `json:"contract"`
	Symbol     string `json:"symbol"`
	Name       string `json:"name"`
	Variant    string `json:"variant"`
	Decimals   int    `json:"decimals"`
	AssetClass string `json:"asset_class"`
	Underlying string `json:"underlying,omitempty"`
	Issuer     string `json:"issuer,omitempty"`
}

const (
	ClassStablecoin      = "stablecoin"
	ClassTokenizedEquity = "tokenized_equity"
)

func Tokens() []Token {
	return []Token{
		{"eip155:1", "0xdac17f958d2ee523a2206206994597c13d831ec7", "USDT", "Tether USD", "issuer-native", 6, ClassStablecoin, "", ""},
		{"eip155:1", "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48", "USDC", "USD Coin", "issuer-native", 6, ClassStablecoin, "", ""},
		{"eip155:56", "0x55d398326f99059ff775485246999027b3197955", "USDT", "Binance-Peg BSC-USD", "binance-peg", 18, ClassStablecoin, "", ""},
		{"eip155:56", "0x8ac76a51cc950d9822d68b83fe1ad97b32cd580d", "USDC", "Binance-Peg USD Coin", "binance-peg", 18, ClassStablecoin, "", ""},
		{"eip155:137", "0xc2132d05d31c914a87c6611c10748aeb04b58e8f", "USDT0", "USDT0 (Polygon USDT)", "usdt0", 6, ClassStablecoin, "", ""},
		{"eip155:137", "0x3c499c542cef5e3811e1192ce70d8cc03d5c3359", "USDC", "USD Coin", "issuer-native", 6, ClassStablecoin, "", ""},
		{"eip155:1", "0x9d275685dc284c8eb1c79f6aba7a63dc75ec890a", "AAPLx", "Apple xStock", "backed-xstock", 18, ClassTokenizedEquity, "AAPL", "Backed Assets"},
		{"eip155:1", "0xcf2810d523c2efb7e4d3afa2629e635d2032ddb4", "TSLAX", "Tesla xStock", "backed-xstock", 18, ClassTokenizedEquity, "TSLA", "Backed Assets"},
		{"eip155:1", "0xe9fa01197b7e836982a1cab03fe591f7e331e313", "AMATx", "Applied Materials, Inc. xStock", "backed-xstock", 18, ClassTokenizedEquity, "AMAT", "Backed Assets"},
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

// FindEquityToken resolves curated tokenized-equity contracts only, keeping the
// stablecoin balance capability separate from RWA reads.
func FindEquityToken(network, contract string) (Token, bool) {
	token, ok := FindToken(network, contract)
	if !ok || token.AssetClass != ClassTokenizedEquity {
		return Token{}, false
	}
	return token, true
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

// EquityTokenRequest targets curated tokenized-equity contracts (data.rwa.token_balance).
type EquityTokenRequest struct {
	Address  string `json:"address"`
	Network  string `json:"network"`
	Provider string `json:"provider,omitempty"`
	Contract string `json:"contract" jsonschema:"Exact allowlisted tokenized-equity contract from GET /v1/tokens entries with asset_class=tokenized_equity"`
}

func (t EquityTokenRequest) Request() (Request, error) {
	r := Request{Address: t.Address, Network: t.Network, Provider: t.Provider, Contract: strings.ToLower(t.Contract)}
	if _, ok := FindEquityToken(r.Network, r.Contract); !ok {
		return r, ErrInput
	}
	return r, r.Validate()
}

type BalancesResult = QueryResult[[]Balance]
