// Package swaps defines read-only quote preparation; signing belongs to the user's wallet.
package swaps

import (
	"0xbot.org/api/internal/data"
	"context"
	"math/big"
	"regexp"
	"strings"
	"time"
)

const Native = "0x0000000000000000000000000000000000000000"

func Networks() []string {
	return []string{"eip155:1", "eip155:10", "eip155:56", "eip155:137", "eip155:8453", "eip155:42161"}
}
func Chain(n string) string {
	for _, v := range Networks() {
		if v == n {
			return strings.TrimPrefix(n, "eip155:")
		}
	}
	return ""
}

var integer = regexp.MustCompile(`^(0|[1-9][0-9]{0,77})$`)

func Amount(s string, positive bool) bool {
	if !integer.MatchString(s) {
		return false
	}
	n, ok := new(big.Int).SetString(s, 10)
	return ok && n.BitLen() <= 256 && (!positive || n.Sign() > 0)
}

var Hash = regexp.MustCompile(`^0x[0-9a-fA-F]{64}$`)

type TokensInput struct {
	Network string `json:"network"`
}

func (r *TokensInput) Validate() error {
	if Chain(r.Network) == "" {
		return data.ErrInput
	}
	return nil
}

type QuoteInput struct {
	FromNetwork string `json:"from_network"`
	ToNetwork   string `json:"to_network"`
	FromToken   string `json:"from_token" jsonschema:"Exact EVM token address; zero address for native coin"`
	ToToken     string `json:"to_token"`
	Amount      string `json:"amount" jsonschema:"Positive uint256 base-unit integer string"`
	Wallet      string `json:"wallet" jsonschema:"Source and recipient EVM wallet; no third-party recipient"`
	SlippageBPS int    `json:"slippage_bps" jsonschema:"10 to 100 basis points (0.1 to 1 percent)"`
}

func (r *QuoteInput) Validate() error {
	if Chain(r.FromNetwork) == "" || Chain(r.ToNetwork) == "" || !data.ValidAddress(r.FromToken) || !data.ValidAddress(r.ToToken) || !data.ValidAddress(r.Wallet) || strings.EqualFold(r.Wallet, Native) || !Amount(r.Amount, true) || r.SlippageBPS < 10 || r.SlippageBPS > 100 || (r.FromNetwork == r.ToNetwork && strings.EqualFold(r.FromToken, r.ToToken)) {
		return data.ErrInput
	}
	r.FromToken = strings.ToLower(r.FromToken)
	r.ToToken = strings.ToLower(r.ToToken)
	r.Wallet = strings.ToLower(r.Wallet)
	return nil
}

type StatusInput struct {
	FromNetwork string `json:"from_network"`
	ToNetwork   string `json:"to_network"`
	TxHash      string `json:"tx_hash"`
}

func (r *StatusInput) Validate() error {
	if Chain(r.FromNetwork) == "" || Chain(r.ToNetwork) == "" || !Hash.MatchString(r.TxHash) {
		return data.ErrInput
	}
	return nil
}

type Token struct {
	Network  string `json:"network"`
	Address  string `json:"address"`
	Symbol   string `json:"symbol"`
	Name     string `json:"name"`
	Decimals int    `json:"decimals"`
}
type Transaction struct {
	Network string `json:"network"`
	From    string `json:"from"`
	To      string `json:"to"`
	Data    string `json:"data"`
	Value   string `json:"value"`
}
type Cost struct {
	Name     string `json:"name"`
	Amount   string `json:"amount"`
	Token    Token  `json:"token"`
	Included bool   `json:"included"`
}
type Quote struct {
	ID              string      `json:"quote_id"`
	Request         QuoteInput  `json:"request"`
	FromToken       Token       `json:"from_token"`
	ToToken         Token       `json:"to_token"`
	ToAmount        string      `json:"to_amount"`
	MinimumAmount   string      `json:"minimum_amount"`
	Tool            string      `json:"tool"`
	ApprovalAddress string      `json:"approval_address"`
	Transaction     Transaction `json:"transaction"`
	Costs           []Cost      `json:"costs"`
	FetchedAt       time.Time   `json:"fetched_at"`
	ExpiresAt       time.Time   `json:"expires_at"`
}
type Status struct {
	TxHash          string `json:"tx_hash"`
	State           string `json:"state" jsonschema:"pending, completed, failed or unknown; source mining alone does not imply cross-chain completion"`
	Substatus       string `json:"substatus"`
	ReceivingTxHash string `json:"receiving_tx_hash"`
}
type TokensResult = data.QueryResult[[]Token]
type QuoteResult = data.QueryResult[Quote]
type StatusResult = data.QueryResult[Status]
type Provider interface {
	Tokens(context.Context, TokensInput) ([]Token, error)
	Quote(context.Context, QuoteInput) (Quote, error)
	Status(context.Context, StatusInput) (Status, error)
}
