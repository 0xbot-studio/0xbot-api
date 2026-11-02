// Package data defines the native balance capability independently of transports and providers.
package data

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
)

var ErrInput = errors.New("invalid_request")
var ErrUnavailable = errors.New("provider_unavailable")
var ErrProvider = errors.New("unsupported_provider")
var ErrRateLimited = errors.New("rate_limited")
var addressPattern = regexp.MustCompile("^0x[0-9a-fA-F]{40}$")

func ValidAddress(s string) bool { return addressPattern.MatchString(s) }

type Request struct {
	Address  string `json:"address" jsonschema:"Ethereum EOA or contract address"`
	Network  string `json:"network" jsonschema:"CAIP-2 network: eip155:1 (Ethereum), eip155:56 (BSC), eip155:137 (Polygon PoS)"`
	Provider string `json:"provider,omitempty" jsonschema:"auto or a route ID from the provider catalog matching the requested network"`
	Contract string `json:"-"`
}

func (r *Request) Validate() error {
	_, supported := FindNetwork(r.Network)
	if !ValidAddress(r.Address) || !supported {
		return ErrInput
	}
	if r.Contract != "" {
		if _, ok := FindToken(r.Network, r.Contract); !ok {
			return ErrInput
		}
	}
	r.Address = strings.ToLower(r.Address)
	if r.Provider == "" {
		r.Provider = "auto"
	}
	return nil
}

type Balance struct {
	Address     string    `json:"address"`
	Network     string    `json:"network"`
	Asset       string    `json:"asset"`
	Symbol      string    `json:"symbol"`
	Decimals    int       `json:"decimals"`
	Amount      string    `json:"amount"`
	BlockNumber string    `json:"block_number"`
	ObservedAt  time.Time `json:"observed_at"`
	Contract    string    `json:"contract,omitempty"`
	Name        string    `json:"name,omitempty"`
	Variant     string    `json:"variant,omitempty"`
}
type BatchProvider interface {
	Balances(context.Context, Request) ([]Balance, error)
}

// QueryResult is the stable v1 success envelope shared by routed read capabilities.
// T preserves capability-specific data; adapters return Balance, not this envelope.
type QueryResult[T any] struct {
	ID        string `json:"id"`
	Provider  string `json:"provider"`
	Status    string `json:"status"`
	LatencyMS int64  `json:"latency_ms"`
	FeeUSD    string `json:"fee_usd"`
	Data      T      `json:"data"`
}

type Result = QueryResult[Balance]
type Provider interface {
	ID() string
	Supports(string) bool
}
