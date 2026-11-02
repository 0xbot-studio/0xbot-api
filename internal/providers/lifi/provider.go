// Package lifi isolates LI.FI's wire format and credentials from public contracts.
package lifi

import (
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/swaps"
	"context"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type cached struct {
	tokens  []swaps.Token
	expires time.Time
}
type Provider struct {
	base, key, integrator string
	client                *http.Client
	mu                    sync.Mutex
	cache                 map[string]cached
	active                chan struct{}
	window                time.Time
	calls                 int
	cooldown              time.Time
	publicAccess          bool
	quoteTimes            []time.Time
	quoteCooldown         time.Time
}

func New(key, integrator string) *Provider {
	return &Provider{base: "https://li.quest/v1", key: key, integrator: integrator, client: &http.Client{Timeout: 18 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, cache: map[string]cached{}, active: make(chan struct{}, 8)}
}

// NewPublic explicitly enables LI.FI's unauthenticated, IP-limited preview tier.
func NewPublic(integrator string) *Provider {
	p := New("", integrator)
	p.publicAccess = true
	return p
}
func (*Provider) ID() string             { return "lifi" }
func (*Provider) Supports(n string) bool { return swaps.Chain(n) != "" }
func (*Provider) CapabilityIDs() []string {
	return []string{"swaps.tokens", "swaps.quote", "swaps.status"}
}
func (p *Provider) get(ctx context.Context, path string, q url.Values, out any) error {
	if p.key == "" && !p.publicAccess {
		return data.ErrUnavailable
	}
	p.mu.Lock()
	now := time.Now()
	if now.Sub(p.window) >= time.Minute {
		p.window = now
		p.calls = 0
	}
	if p.calls >= 90 || now.Before(p.cooldown) {
		p.mu.Unlock()
		return data.ErrRateLimited
	}
	if path == "/quote" {
		if now.Before(p.quoteCooldown) {
			p.mu.Unlock()
			return data.ErrRateLimited
		}
		if p.key == "" {
			cutoff := now.Add(-2 * time.Hour)
			for len(p.quoteTimes) > 0 && !p.quoteTimes[0].After(cutoff) {
				p.quoteTimes = p.quoteTimes[1:]
			}
			// Leave headroom below LI.FI's documented 75/IP/two-hour rolling quota.
			if len(p.quoteTimes) >= 70 {
				p.mu.Unlock()
				return data.ErrRateLimited
			}
			p.quoteTimes = append(p.quoteTimes, now)
		}
	}
	p.calls++
	p.mu.Unlock()
	select {
	case p.active <- struct{}{}:
		defer func() { <-p.active }()
	default:
		return data.ErrUnavailable
	}
	req, e := http.NewRequestWithContext(ctx, "GET", p.base+path+"?"+q.Encode(), nil)
	if e != nil {
		return data.ErrUnavailable
	}
	if p.key != "" {
		req.Header.Set("x-lifi-api-key", p.key)
	}
	res, e := p.client.Do(req)
	if e != nil {
		return data.ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode == 429 {
		p.mu.Lock()
		delay := 60
		for _, header := range []string{"Retry-After", "ratelimit-reset"} {
			if seconds, e := strconv.Atoi(res.Header.Get(header)); e == nil && seconds > 0 {
				delay = min(seconds, 7200)
				break
			}
		}
		until := time.Now().Add(time.Duration(delay) * time.Second)
		if path == "/quote" {
			p.quoteCooldown = until
		} else {
			p.cooldown = until
		}
		p.mu.Unlock()
		return data.ErrRateLimited
	}
	if res.StatusCode != 200 {
		return data.ErrUnavailable
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, (12<<20)+1))
	if e != nil || len(b) > 12<<20 || json.Unmarshal(b, out) != nil {
		return data.ErrUnavailable
	}
	return nil
}

type wireToken struct {
	Address, Symbol, Name string
	Decimals              *int
	ChainID               int `json:"chainId"`
}

func (t wireToken) normalized() (swaps.Token, error) {
	n := "eip155:" + strconv.Itoa(t.ChainID)
	if swaps.Chain(n) == "" || !data.ValidAddress(t.Address) || t.Decimals == nil || *t.Decimals < 0 || *t.Decimals > 36 || t.Symbol == "" || len(t.Symbol) > 80 || len(t.Name) > 256 {
		return swaps.Token{}, data.ErrUnavailable
	}
	return swaps.Token{Network: n, Address: strings.ToLower(t.Address), Symbol: t.Symbol, Name: t.Name, Decimals: *t.Decimals}, nil
}
func (p *Provider) Tokens(ctx context.Context, in swaps.TokensInput) ([]swaps.Token, error) {
	if e := in.Validate(); e != nil {
		return nil, e
	}
	p.mu.Lock()
	c, ok := p.cache[in.Network]
	p.mu.Unlock()
	if ok && time.Now().Before(c.expires) {
		return append([]swaps.Token{}, c.tokens...), nil
	}
	var wire struct{ Tokens map[string][]wireToken }
	if e := p.get(ctx, "/tokens", url.Values{"chains": {swaps.Chain(in.Network)}}, &wire); e != nil {
		return nil, e
	}
	out := []swaps.Token{}
	seen := map[string]bool{}
	for _, t := range wire.Tokens[swaps.Chain(in.Network)] {
		v, e := t.normalized()
		if e == nil && v.Network == in.Network && !seen[v.Address] {
			out = append(out, v)
			seen[v.Address] = true
		}
	}
	if len(out) == 0 {
		return nil, data.ErrUnavailable
	}
	p.mu.Lock()
	p.cache[in.Network] = cached{out, time.Now().Add(5 * time.Minute)}
	p.mu.Unlock()
	return append([]swaps.Token{}, out...), nil
}

var calldata = regexp.MustCompile(`^0x(?:[0-9a-fA-F]{2})+$`)

func decimalValue(s string) (string, bool) {
	n, ok := new(big.Int).SetString(s, 0)
	if !strings.HasPrefix(s, "0x") {
		n, ok = new(big.Int).SetString(s, 10)
	}
	if !ok || n.Sign() < 0 || n.BitLen() > 256 {
		return "", false
	}
	return n.String(), true
}
func (p *Provider) Quote(ctx context.Context, in swaps.QuoteInput) (swaps.Quote, error) {
	var out swaps.Quote
	if e := in.Validate(); e != nil {
		return out, e
	}
	var wire struct {
		ID, Tool string
		Action   struct {
			FromChainID int         `json:"fromChainId"`
			ToChainID   int         `json:"toChainId"`
			FromAmount  string      `json:"fromAmount"`
			FromAddress string      `json:"fromAddress"`
			ToAddress   string      `json:"toAddress"`
			FromToken   wireToken   `json:"fromToken"`
			ToToken     wireToken   `json:"toToken"`
			Slippage    json.Number `json:"slippage"`
		}
		Estimate struct {
			FromAmount      string `json:"fromAmount"`
			ToAmount        string `json:"toAmount"`
			ToAmountMin     string `json:"toAmountMin"`
			ApprovalAddress string `json:"approvalAddress"`
			FeeCosts        []struct {
				Name, Amount string
				Token        wireToken
				Included     bool
			} `json:"feeCosts"`
			GasCosts []struct {
				Type, Amount string
				Token        wireToken
			} `json:"gasCosts"`
		}
		Transaction struct {
			From, To, Data, Value string
			ChainID               int `json:"chainId"`
		} `json:"transactionRequest"`
	}
	q := url.Values{"fromChain": {swaps.Chain(in.FromNetwork)}, "toChain": {swaps.Chain(in.ToNetwork)}, "fromToken": {in.FromToken}, "toToken": {in.ToToken}, "fromAmount": {in.Amount}, "fromAddress": {in.Wallet}, "toAddress": {in.Wallet}, "slippage": {fmtBPS(in.SlippageBPS)}, "integrator": {p.integrator}, "order": {"CHEAPEST"}}
	if e := p.get(ctx, "/quote", q, &wire); e != nil {
		return out, e
	}
	a, x, t := wire.Action, wire.Estimate, wire.Transaction
	from, e1 := a.FromToken.normalized()
	to, e2 := a.ToToken.normalized()
	value, ok := decimalValue(t.Value)
	if e1 != nil || e2 != nil || wire.ID == "" || len(wire.ID) > 256 || wire.Tool == "" || len(wire.Tool) > 128 || from.Network != in.FromNetwork || to.Network != in.ToNetwork || strconv.Itoa(a.FromChainID) != swaps.Chain(in.FromNetwork) || strconv.Itoa(a.ToChainID) != swaps.Chain(in.ToNetwork) || from.Address != in.FromToken || to.Address != in.ToToken || a.FromAmount != in.Amount || x.FromAmount != in.Amount || !strings.EqualFold(a.FromAddress, in.Wallet) || !strings.EqualFold(a.ToAddress, in.Wallet) || !swaps.Amount(x.ToAmount, true) || !swaps.Amount(x.ToAmountMin, true) || !ok || strconv.Itoa(t.ChainID) != swaps.Chain(in.FromNetwork) || !strings.EqualFold(t.From, in.Wallet) || !data.ValidAddress(t.To) || strings.EqualFold(t.To, swaps.Native) || len(t.Data) < 10 || len(t.Data) > 131074 || !calldata.MatchString(t.Data) {
		return out, data.ErrUnavailable
	}
	slippage, validSlippage := new(big.Rat).SetString(string(a.Slippage))
	if !validSlippage || slippage.Cmp(big.NewRat(int64(in.SlippageBPS), 10000)) != 0 {
		return out, data.ErrUnavailable
	}
	max, _ := new(big.Int).SetString(x.ToAmount, 10)
	min, _ := new(big.Int).SetString(x.ToAmountMin, 10)
	if min.Cmp(max) > 0 {
		return out, data.ErrUnavailable
	}
	if in.FromToken != swaps.Native && (!data.ValidAddress(x.ApprovalAddress) || strings.EqualFold(x.ApprovalAddress, swaps.Native)) {
		return out, data.ErrUnavailable
	}
	if in.FromToken == swaps.Native {
		v, _ := new(big.Int).SetString(value, 10)
		amount, _ := new(big.Int).SetString(in.Amount, 10)
		if v.Cmp(amount) < 0 {
			return out, data.ErrUnavailable
		}
		x.ApprovalAddress = ""
	}
	costs := []swaps.Cost{}
	maximumValue := new(big.Int)
	if in.FromToken == swaps.Native {
		maximumValue.SetString(in.Amount, 10)
	}
	for _, c := range x.FeeCosts {
		token, e := c.Token.normalized()
		if e != nil || !swaps.Amount(c.Amount, false) || len(c.Name) > 128 {
			return out, data.ErrUnavailable
		}
		if !c.Included && token.Network == in.FromNetwork && token.Address == swaps.Native {
			amount, _ := new(big.Int).SetString(c.Amount, 10)
			maximumValue.Add(maximumValue, amount)
		}
		costs = append(costs, swaps.Cost{Name: c.Name, Amount: c.Amount, Token: token, Included: c.Included})
	}
	for _, c := range x.GasCosts {
		token, e := c.Token.normalized()
		if e != nil || !swaps.Amount(c.Amount, false) {
			return out, data.ErrUnavailable
		}
		costs = append(costs, swaps.Cost{Name: "gas", Amount: c.Amount, Token: token})
	}
	actualValue, _ := new(big.Int).SetString(value, 10)
	if actualValue.Cmp(maximumValue) > 0 {
		return out, data.ErrUnavailable
	}
	now := time.Now().UTC()
	return swaps.Quote{ID: wire.ID, Request: in, FromToken: from, ToToken: to, ToAmount: x.ToAmount, MinimumAmount: x.ToAmountMin, Tool: wire.Tool, ApprovalAddress: strings.ToLower(x.ApprovalAddress), Transaction: swaps.Transaction{Network: in.FromNetwork, From: in.Wallet, To: strings.ToLower(t.To), Data: t.Data, Value: value}, Costs: costs, FetchedAt: now, ExpiresAt: now.Add(time.Minute)}, nil
}
func fmtBPS(b int) string { return "0." + strconv.FormatInt(int64(10000+b), 10)[1:] }
func (p *Provider) Status(ctx context.Context, in swaps.StatusInput) (swaps.Status, error) {
	out := swaps.Status{TxHash: in.TxHash, State: "unknown"}
	if e := in.Validate(); e != nil {
		return out, e
	}
	var wire struct {
		Status, Substatus string
		Sending           struct {
			TxHash  string `json:"txHash"`
			ChainID int    `json:"chainId"`
		}
		Receiving struct {
			TxHash  string `json:"txHash"`
			ChainID int    `json:"chainId"`
		}
	}
	if e := p.get(ctx, "/status", url.Values{"txHash": {in.TxHash}, "fromChain": {swaps.Chain(in.FromNetwork)}, "toChain": {swaps.Chain(in.ToNetwork)}}, &wire); e != nil {
		return out, e
	}
	if wire.Status == "NOT_FOUND" || wire.Status == "INVALID" {
		return out, nil
	}
	if !strings.EqualFold(wire.Sending.TxHash, in.TxHash) || strconv.Itoa(wire.Sending.ChainID) != swaps.Chain(in.FromNetwork) {
		return out, data.ErrUnavailable
	}
	switch wire.Status {
	case "PENDING":
		out.State = "pending"
	case "FAILED":
		out.State = "failed"
	case "DONE":
		if wire.Substatus == "COMPLETED" && swaps.Hash.MatchString(wire.Receiving.TxHash) && strconv.Itoa(wire.Receiving.ChainID) == swaps.Chain(in.ToNetwork) {
			out.State = "completed"
		}
	}
	if len(wire.Substatus) <= 80 {
		out.Substatus = wire.Substatus
	}
	if swaps.Hash.MatchString(wire.Receiving.TxHash) {
		out.ReceivingTxHash = wire.Receiving.TxHash
	}
	return out, nil
}
