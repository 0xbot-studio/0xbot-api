// Package evmrpc adapts the Ethereum JSON-RPC contract to the platform balance capability.
package evmrpc

import (
	"0xbot.org/api/internal/capabilities"
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/nft"
	"0xbot.org/api/internal/providers/nftmetadata"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"
)

type Provider struct {
	id, endpoint string
	client       *http.Client
	network      string
	metadata     nft.MetadataReader
}

func New(id, endpoint string, client *http.Client) *Provider {
	return NewForNetwork(id, endpoint, "eip155:1", client)
}
func NewForNetwork(id, endpoint, network string, client *http.Client) *Provider {
	return &Provider{id: id, endpoint: endpoint, client: client, network: network, metadata: nftmetadata.New()}
}
func (p *Provider) Supports(network string) bool { return p.network == network }
func (p *Provider) ID() string                   { return p.id }
func (p *Provider) CapabilityIDs() []string {
	return []string{capabilities.NativeBalance, capabilities.TokenBalance, capabilities.CommonBalances, capabilities.ResolveTokenBoundAccount, capabilities.GetTokenBoundAccount, capabilities.NFTDetail, capabilities.NFTOwner, capabilities.NFTBalance}
}
func (p *Provider) rpc(ctx context.Context, method string, params []any) (string, error) {
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if e != nil {
		return "", data.ErrUnavailable
	}
	req.Header.Set("Content-Type", "application/json")
	res, e := p.client.Do(req)
	if e != nil {
		return "", data.ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", data.ErrUnavailable
	}
	var out struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      int             `json:"id"`
		Result  string          `json:"result"`
		Error   json.RawMessage `json:"error"`
	}
	if e = json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&out); e != nil || out.JSONRPC != "2.0" || out.ID != 1 {
		return "", data.ErrUnavailable
	}
	if len(out.Error) > 0 && string(out.Error) != "null" {
		var rpcErr struct{ Code int }
		if json.Unmarshal(out.Error, &rpcErr) == nil && rpcErr.Code == 3 {
			return "", nft.ErrReverted
		}
		return "", data.ErrUnavailable
	}
	return out.Result, nil
}
func (p *Provider) call(ctx context.Context, method string, params []any) (string, error) {
	raw, e := p.rpc(ctx, method, params)
	if e != nil {
		// Preserve existing balance/account error semantics. NFT calls classify reverts separately.
		if errors.Is(e, nft.ErrReverted) {
			return "", data.ErrUnavailable
		}
		return "", e
	}
	out := struct{ Result string }{raw}
	if method == "eth_getCode" {
		if !strings.HasPrefix(out.Result, "0x") {
			return "", data.ErrUnavailable
		}
		if _, e = hex.DecodeString(out.Result[2:]); e != nil {
			return "", data.ErrUnavailable
		}
		return out.Result, nil
	}
	if method == "eth_call" {
		if _, e = abiWord(out.Result); e != nil {
			return "", e
		}
		return out.Result, nil
	}
	if _, e = quantity(out.Result); e != nil {
		return "", e
	}
	return out.Result, nil
}
func quantity(s string) (*big.Int, error) {
	if !strings.HasPrefix(s, "0x") || len(s) < 3 || len(s) > 66 {
		return nil, data.ErrUnavailable
	}
	if len(s) > 3 && s[2] == '0' {
		return nil, data.ErrUnavailable
	}
	for _, c := range s[2:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return nil, data.ErrUnavailable
		}
	}
	n, ok := new(big.Int).SetString(s[2:], 16)
	if !ok {
		return nil, data.ErrUnavailable
	}
	return n, nil
}
func (p *Provider) Balance(ctx context.Context, r data.Request) (data.Balance, error) {
	block, e := p.block(ctx, r)
	if e != nil {
		return data.Balance{}, e
	}
	return p.balanceAt(ctx, r, block)
}

func (p *Provider) block(ctx context.Context, r data.Request) (string, error) {
	network, ok := data.FindNetwork(r.Network)
	if !ok || !p.Supports(r.Network) {
		return "", data.ErrInput
	}
	chain, e := p.call(ctx, "eth_chainId", []any{})
	if e != nil || chain != network.ChainHex {
		return "", data.ErrUnavailable
	}
	return p.call(ctx, "eth_blockNumber", []any{})
}

func (p *Provider) balanceAt(ctx context.Context, r data.Request, block string) (data.Balance, error) {
	if r.Contract != "" {
		return p.tokenAt(ctx, r, block)
	}
	network, _ := data.FindNetwork(r.Network)
	amount, e := p.call(ctx, "eth_getBalance", []any{r.Address, block})
	if e != nil {
		return data.Balance{}, e
	}
	n, e := quantity(amount)
	if e != nil {
		return data.Balance{}, e
	}
	b, e := quantity(block)
	if e != nil {
		return data.Balance{}, e
	}
	return data.Balance{Address: r.Address, Network: r.Network, Asset: network.Asset, Symbol: network.Symbol, Decimals: 18, Amount: n.String(), BlockNumber: b.String(), ObservedAt: time.Now().UTC()}, nil
}

func abiWord(s string) (*big.Int, error) {
	if len(s) != 66 || !strings.HasPrefix(s, "0x") {
		return nil, data.ErrUnavailable
	}
	b, err := hex.DecodeString(s[2:])
	if err != nil {
		return nil, data.ErrUnavailable
	}
	return new(big.Int).SetBytes(b), nil
}

func (p *Provider) tokenAt(ctx context.Context, r data.Request, block string) (data.Balance, error) {
	token, ok := data.FindToken(r.Network, r.Contract)
	if !ok || !data.ValidAddress(r.Address) {
		return data.Balance{}, data.ErrInput
	}
	call := func(input string) (*big.Int, error) {
		raw, err := p.call(ctx, "eth_call", []any{map[string]string{"to": token.Contract, "data": input}, block})
		if err != nil {
			return nil, err
		}
		return abiWord(raw)
	}
	decimals, err := call("0x313ce567")
	if err != nil || decimals.Cmp(big.NewInt(int64(token.Decimals))) != 0 {
		return data.Balance{}, data.ErrUnavailable
	}
	amount, err := call("0x70a08231" + strings.Repeat("0", 24) + strings.ToLower(r.Address[2:]))
	if err != nil {
		return data.Balance{}, err
	}
	b, err := quantity(block)
	if err != nil {
		return data.Balance{}, err
	}
	return data.Balance{Address: r.Address, Network: r.Network, Asset: r.Network + "/erc20:" + token.Contract, Symbol: token.Symbol, Decimals: token.Decimals, Amount: amount.String(), BlockNumber: b.String(), ObservedAt: time.Now().UTC(), Contract: token.Contract, Name: token.Name, Variant: token.Variant}, nil
}

// All three reads use one provider and one block. Never return partial results
// as a successful snapshot; automatic routing retries the complete read set.
func (p *Provider) Balances(ctx context.Context, r data.Request) ([]data.Balance, error) {
	if err := r.Validate(); err != nil || r.Contract != "" {
		return nil, data.ErrInput
	}
	block, err := p.block(ctx, r)
	if err != nil {
		return nil, err
	}
	native, err := p.balanceAt(ctx, r, block)
	if err != nil {
		return nil, err
	}
	out := []data.Balance{native}
	for _, token := range data.Tokens() {
		if token.Network != r.Network {
			continue
		}
		r.Contract = token.Contract
		b, err := p.tokenAt(ctx, r, block)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

var _ data.Provider = (*Provider)(nil)
