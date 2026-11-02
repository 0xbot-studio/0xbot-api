// Package prices owns reference market quotes, not balances or execution prices.
package prices

import (
	"0xbot.org/api/internal/data"
	"context"
	"errors"
	"strings"
	"time"
)

var ErrNotFound = errors.New("price_not_found")
var ErrStale = errors.New("price_stale")
var ErrConfidence = errors.New("price_low_confidence")

type AssetRequest struct {
	Asset    string `json:"asset" jsonschema:"BTC, ETH or BNB; native asset, not wrapped token"`
	Provider string `json:"provider,omitempty" jsonschema:"Omit for automatic price routing"`
}

func (r *AssetRequest) Validate() error {
	r.Asset = strings.ToUpper(strings.TrimSpace(r.Asset))
	if r.Asset != "BTC" && r.Asset != "ETH" && r.Asset != "BNB" {
		return data.ErrInput
	}
	if r.Provider == "" {
		r.Provider = "auto"
	}
	return nil
}

type TokenRequest struct {
	Network  string `json:"network" jsonschema:"eip155:1, eip155:56 or eip155:137"`
	Contract string `json:"contract" jsonschema:"Exact nonzero EVM token contract, never a ticker"`
	Provider string `json:"provider,omitempty"`
}

func (r *TokenRequest) Validate() error {
	q := data.Request{Address: r.Contract, Network: r.Network, Provider: r.Provider}
	if q.Validate() != nil || q.Address == "0x"+strings.Repeat("0", 40) {
		return data.ErrInput
	}
	r.Contract, r.Provider = q.Address, q.Provider
	return nil
}

// EquityRequest prices the underlying stock of a curated tokenized-equity
// contract (market.equity_price). The registry lookup keeps ticker guessing out.
type EquityRequest struct {
	Network  string `json:"network" jsonschema:"Network of a curated tokenized-equity contract; Ethereum eip155:1 in the first release"`
	Contract string `json:"contract" jsonschema:"Exact curated tokenized-equity contract with asset_class=tokenized_equity; never a stock ticker"`
	Provider string `json:"provider,omitempty"`
}

func (r *EquityRequest) Validate() error {
	token, ok := data.FindEquityToken(r.Network, r.Contract)
	if !ok {
		return data.ErrInput
	}
	if _, supported := data.FindNetwork(token.Network); !supported {
		return data.ErrInput
	}
	r.Contract, r.Network = token.Contract, token.Network
	if r.Provider == "" {
		r.Provider = "auto"
	}
	return nil
}

type Quote struct {
	Asset       string    `json:"asset" jsonschema:"BTC/ETH/BNB for asset quote, empty for contract quote"`
	Network     string    `json:"network" jsonschema:"Empty for global asset quote; otherwise CAIP-2 network"`
	Contract    string    `json:"contract"`
	Symbol      string    `json:"symbol" jsonschema:"Informational only; not a unique identity"`
	Currency    string    `json:"currency" jsonschema:"USD; not USDT"`
	Price       string    `json:"price" jsonschema:"Exact positive decimal string in USD per whole token; do not scale by token decimals"`
	Source      string    `json:"source"`
	SourceAsset string    `json:"source_asset" jsonschema:"Upstream asset identifier for provenance"`
	Confidence  string    `json:"confidence" jsonschema:"Provider confidence score, not a safety assessment"`
	UpdatedAt   time.Time `json:"updated_at" jsonschema:"Price observation timestamp reported by source"`
	FetchedAt   time.Time `json:"fetched_at" jsonschema:"Time fetched by 0xbot; not quote update time"`
	Cached      bool      `json:"cached"`
}
type Result = data.QueryResult[Quote]
type Provider interface {
	ID() string
	Supports(string) bool
	AssetPrice(context.Context, AssetRequest) (Quote, error)
	TokenPrice(context.Context, TokenRequest) (Quote, error)
}
