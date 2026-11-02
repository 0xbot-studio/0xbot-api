// Package tokenbound defines read-only ERC-6551 capabilities, independent of RPC.
package tokenbound

import (
	"context"
	"errors"
	"math/big"
	"regexp"
	"strings"
	"time"

	"0xbot.org/api/internal/data"
)

const Registry = "0x000000006551c19487814612e58fe06813775758"

var ErrNotAccount = errors.New("not_token_bound_account")
var saltPattern = regexp.MustCompile(`^0x[0-9a-fA-F]{64}$`)
var uintPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,77})$`)

type ResolveRequest struct {
	Network        string `json:"network" jsonschema:"Account and NFT network (same-chain only): eip155:1, eip155:56 or eip155:137"`
	TokenContract  string `json:"token_contract" jsonschema:"ERC-721 contract address; NFT existence is not verified"`
	TokenID        string `json:"token_id" jsonschema:"Canonical unsigned decimal uint256 string, not a JSON number"`
	Implementation string `json:"implementation" jsonschema:"Explicit account implementation address; never inferred or recommended"`
	Salt           string `json:"salt" jsonschema:"Explicit 0x-prefixed 32-byte salt; no implicit default"`
	Provider       string `json:"provider,omitempty" jsonschema:"Omit for automatic routing, or select a provider supporting the network"`
}

func (r *ResolveRequest) Validate() error {
	base := data.Request{Address: r.TokenContract, Network: r.Network, Provider: r.Provider}
	if base.Validate() != nil || !data.ValidAddress(r.Implementation) || !saltPattern.MatchString(r.Salt) || !uintPattern.MatchString(r.TokenID) {
		return data.ErrInput
	}
	n, ok := new(big.Int).SetString(r.TokenID, 10)
	if !ok || n.BitLen() > 256 || strings.EqualFold(r.Implementation, "0x"+strings.Repeat("0", 40)) || strings.EqualFold(r.TokenContract, "0x"+strings.Repeat("0", 40)) {
		return data.ErrInput
	}
	r.TokenContract, r.Provider = base.Address, base.Provider
	r.Implementation, r.Salt = strings.ToLower(r.Implementation), strings.ToLower(r.Salt)
	return nil
}

type GetRequest struct {
	Address  string `json:"address" jsonschema:"Deployed canonical ERC-6551 token-bound account address"`
	Network  string `json:"network" jsonschema:"Account and NFT network (same-chain only): eip155:1, eip155:56 or eip155:137"`
	Provider string `json:"provider,omitempty" jsonschema:"Omit for automatic routing"`
}

func (r *GetRequest) Validate() error {
	base := data.Request{Address: r.Address, Network: r.Network, Provider: r.Provider}
	if err := base.Validate(); err != nil {
		return err
	}
	r.Address, r.Provider = base.Address, base.Provider
	return nil
}

// Verification concerns proxy structure and deterministic address only. It is
// not an audit of the implementation or proof of NFT existence/ownership.
type Account struct {
	Address                string    `json:"address"`
	Network                string    `json:"network"`
	Registry               string    `json:"registry"`
	TokenContract          string    `json:"token_contract"`
	TokenID                string    `json:"token_id"`
	Implementation         string    `json:"implementation"`
	Salt                   string    `json:"salt"`
	Deployed               bool      `json:"deployed"`
	ImplementationDeployed bool      `json:"implementation_deployed"`
	Verification           string    `json:"verification" jsonschema:"counterfactual or canonical_proxy; not proof of safety, ERC-721 existence or authorization"`
	BlockNumber            string    `json:"block_number"`
	ObservedAt             time.Time `json:"observed_at"`
}
type Result = data.QueryResult[Account]
type Provider interface {
	ResolveTokenBoundAccount(context.Context, ResolveRequest) (Account, error)
	GetTokenBoundAccount(context.Context, GetRequest) (Account, error)
}
