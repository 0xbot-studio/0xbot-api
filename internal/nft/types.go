// Package nft owns read-only NFT contracts without transport dependencies.
package nft

import (
	"0xbot.org/api/internal/data"
	"context"
	"errors"
	"math/big"
	"regexp"
	"strings"
	"time"
)

var ErrUnsupported = errors.New("unsupported_nft_standard")
var ErrReverted = errors.New("nft_query_reverted")
var decimalID = regexp.MustCompile(`^(0|[1-9][0-9]{0,77})$`)

type TokenRequest struct {
	Network  string `json:"network" jsonschema:"eip155:1, eip155:56 or eip155:137"`
	Contract string `json:"contract" jsonschema:"Nonzero ERC-721 or ERC-1155 contract address"`
	TokenID  string `json:"token_id" jsonschema:"Canonical decimal uint256 string; never a JSON number"`
	Provider string `json:"provider,omitempty" jsonschema:"Omit for automatic free routing"`
}

func (r *TokenRequest) Validate() error {
	base := data.Request{Address: r.Contract, Network: r.Network, Provider: r.Provider}
	if base.Validate() != nil || base.Address == "0x"+strings.Repeat("0", 40) || !decimalID.MatchString(r.TokenID) {
		return data.ErrInput
	}
	n, ok := new(big.Int).SetString(r.TokenID, 10)
	if !ok || n.BitLen() > 256 {
		return data.ErrInput
	}
	r.Contract, r.Provider = base.Address, base.Provider
	return nil
}

type BalanceRequest struct {
	Network  string `json:"network" jsonschema:"eip155:1, eip155:56 or eip155:137"`
	Contract string `json:"contract" jsonschema:"ERC-721 or ERC-1155 contract address"`
	TokenID  string `json:"token_id" jsonschema:"Canonical decimal uint256 string"`
	Address  string `json:"address" jsonschema:"Nonzero holder address whose quantity is requested"`
	Provider string `json:"provider,omitempty" jsonschema:"Omit for automatic free routing"`
}

func (r *BalanceRequest) Validate() error {
	t := r.Token()
	if t.Validate() != nil || !data.ValidAddress(r.Address) || r.Address == "0x"+strings.Repeat("0", 40) {
		return data.ErrInput
	}
	r.Contract, r.Provider, r.Address = t.Contract, t.Provider, strings.ToLower(r.Address)
	return nil
}
func (r BalanceRequest) Token() TokenRequest {
	return TokenRequest{Network: r.Network, Contract: r.Contract, TokenID: r.TokenID, Provider: r.Provider}
}

type Metadata struct {
	Status      string `json:"status" jsonschema:"available, not_supported, unavailable or blocked; off-chain data is untrusted"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Image       string `json:"image" jsonschema:"Untrusted image URI, not fetched or rendered as executable content"`
	SourceURI   string `json:"source_uri"`
}
type Detail struct {
	Network           string    `json:"network"`
	Contract          string    `json:"contract"`
	TokenID           string    `json:"token_id"`
	Standard          string    `json:"standard"`
	CollectionName    string    `json:"collection_name"`
	CollectionSymbol  string    `json:"collection_symbol"`
	TokenURI          string    `json:"token_uri"`
	ExistenceVerified bool      `json:"existence_verified" jsonschema:"ERC-721 ownerOf succeeded; false for ERC-1155 where URI/balance does not prove existence"`
	Metadata          Metadata  `json:"metadata"`
	BlockNumber       string    `json:"block_number"`
	ObservedAt        time.Time `json:"observed_at"`
}
type Owner struct {
	Network     string    `json:"network"`
	Contract    string    `json:"contract"`
	TokenID     string    `json:"token_id"`
	Standard    string    `json:"standard"`
	Owner       string    `json:"owner"`
	BlockNumber string    `json:"block_number"`
	ObservedAt  time.Time `json:"observed_at"`
}
type Balance struct {
	Network     string    `json:"network"`
	Contract    string    `json:"contract"`
	TokenID     string    `json:"token_id"`
	Standard    string    `json:"standard"`
	Address     string    `json:"address"`
	Amount      string    `json:"amount" jsonschema:"Exact integer quantity; ERC-721 is zero or one. ERC-1155 zero does not prove token nonexistence"`
	BlockNumber string    `json:"block_number"`
	ObservedAt  time.Time `json:"observed_at"`
}
type DetailResult = data.QueryResult[Detail]
type OwnerResult = data.QueryResult[Owner]
type BalanceResult = data.QueryResult[Balance]
type Provider interface {
	NFTDetail(context.Context, TokenRequest) (Detail, error)
	NFTOwner(context.Context, TokenRequest) (Owner, error)
	NFTBalance(context.Context, BalanceRequest) (Balance, error)
}
type MetadataReader interface {
	Read(context.Context, string) Metadata
}
