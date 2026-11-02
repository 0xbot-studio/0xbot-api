// Package llamaprices adapts public reference prices without API credentials.
package llamaprices

import (
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/prices"
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

type Provider struct {
	client *http.Client
	base   string
	mu     sync.Mutex
	cache  map[string]prices.Quote
	window time.Time
	calls  int
}

func New() *Provider {
	return newProvider("https://coins.llama.fi", &http.Client{Timeout: 6 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }})
}
func newProvider(base string, client *http.Client) *Provider {
	return &Provider{base: base, client: client, cache: map[string]prices.Quote{}}
}
func (p *Provider) ID() string { return "defillama-prices" }
func (p *Provider) Supports(n string) bool {
	return n == "market" || n == "eip155:1" || n == "eip155:56" || n == "eip155:137"
}
func (p *Provider) CapabilityIDs() []string {
	return []string{"market.asset_price", "market.token_price"}
}

var decimalPattern = regexp.MustCompile(`^(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)

// Preserve JSON numeric precision including exponent notation, with bounded expansion.
func decimal(s string) (string, error) {
	if len(s) > 128 || !decimalPattern.MatchString(s) {
		return "", data.ErrUnavailable
	}
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		exponent, err := strconv.Atoi(s[i+1:])
		if err != nil || exponent < -40 || exponent > 40 {
			return "", data.ErrUnavailable
		}
	}
	n, ok := new(big.Rat).SetString(s)
	if !ok || n.Sign() <= 0 || n.Cmp(new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(40), nil))) > 0 {
		return "", data.ErrUnavailable
	}
	// Bound exponent before formatting; 40 fractional digits preserves supported small-token prices.
	if n.Cmp(new(big.Rat).SetFrac(big.NewInt(1), new(big.Int).Exp(big.NewInt(10), big.NewInt(40), nil))) < 0 {
		return "", data.ErrUnavailable
	}
	value := strings.TrimRight(strings.TrimRight(n.FloatString(40), "0"), ".")
	check, _ := new(big.Rat).SetString(value)
	if check == nil || check.Cmp(n) != 0 {
		return "", data.ErrUnavailable
	}
	return value, nil
}
func (p *Provider) AssetPrice(ctx context.Context, in prices.AssetRequest) (prices.Quote, error) {
	if err := in.Validate(); err != nil {
		return prices.Quote{}, err
	}
	id := map[string]string{"BTC": "bitcoin", "ETH": "ethereum", "BNB": "binancecoin"}[in.Asset]
	q, err := p.fetch(ctx, "coingecko:"+id)
	if err != nil {
		return prices.Quote{}, err
	}
	if q.Symbol != in.Asset {
		return prices.Quote{}, data.ErrUnavailable
	}
	q.Asset = in.Asset
	return q, nil
}
func (p *Provider) TokenPrice(ctx context.Context, in prices.TokenRequest) (prices.Quote, error) {
	if err := in.Validate(); err != nil {
		return prices.Quote{}, err
	}
	chain := map[string]string{"eip155:1": "ethereum", "eip155:56": "bsc", "eip155:137": "polygon"}[in.Network]
	q, err := p.fetch(ctx, chain+":"+in.Contract)
	if err != nil {
		return prices.Quote{}, err
	}
	q.Network, q.Contract = in.Network, in.Contract
	return q, nil
}
func (p *Provider) fetch(ctx context.Context, id string) (prices.Quote, error) {
	if err := ctx.Err(); err != nil {
		return prices.Quote{}, err
	}
	now := time.Now().UTC()
	p.mu.Lock()
	if q, ok := p.cache[id]; ok && now.Sub(q.FetchedAt) < 30*time.Second && now.Sub(q.UpdatedAt) <= 10*time.Minute {
		p.mu.Unlock()
		q.Cached = true
		return q, nil
	}
	if now.Sub(p.window) >= time.Minute {
		p.window = now
		p.calls = 0
	}
	if p.calls >= 60 {
		p.mu.Unlock()
		return prices.Quote{}, data.ErrRateLimited
	}
	p.calls++
	p.mu.Unlock()
	req, err := http.NewRequestWithContext(ctx, "GET", p.base+"/prices/current/"+url.PathEscape(id), nil)
	if err != nil {
		return prices.Quote{}, data.ErrUnavailable
	}
	res, err := p.client.Do(req)
	if err != nil {
		return prices.Quote{}, data.ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode == 429 {
		return prices.Quote{}, data.ErrRateLimited
	}
	if res.StatusCode != 200 {
		return prices.Quote{}, data.ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, (64<<10)+1))
	if err != nil || len(raw) > 64<<10 {
		return prices.Quote{}, data.ErrUnavailable
	}
	var payload struct {
		Coins map[string]struct {
			Price      json.Number `json:"price"`
			Symbol     string      `json:"symbol"`
			Timestamp  int64       `json:"timestamp"`
			Confidence json.Number `json:"confidence"`
		} `json:"coins"`
	}
	if json.Unmarshal(raw, &payload) != nil || payload.Coins == nil {
		return prices.Quote{}, data.ErrUnavailable
	}
	item, ok := payload.Coins[id]
	if !ok {
		return prices.Quote{}, prices.ErrNotFound
	}
	value, err := decimal(item.Price.String())
	if err != nil {
		return prices.Quote{}, err
	}
	confidence, err := decimal(item.Confidence.String())
	if err != nil {
		return prices.Quote{}, prices.ErrConfidence
	}
	score, _ := new(big.Rat).SetString(confidence)
	if score.Cmp(big.NewRat(9, 10)) < 0 || score.Cmp(big.NewRat(1, 1)) > 0 {
		return prices.Quote{}, prices.ErrConfidence
	}
	now = time.Now().UTC()
	updated := time.Unix(item.Timestamp, 0).UTC()
	if item.Timestamp <= 0 || updated.After(now.Add(time.Minute)) {
		return prices.Quote{}, data.ErrUnavailable
	}
	if now.Sub(updated) > 10*time.Minute {
		return prices.Quote{}, prices.ErrStale
	}
	if item.Symbol == "" || len(item.Symbol) > 64 {
		return prices.Quote{}, data.ErrUnavailable
	}
	q := prices.Quote{Price: value, Currency: "USD", Symbol: item.Symbol, Source: "DefiLlama", SourceAsset: id, Confidence: confidence, UpdatedAt: updated, FetchedAt: now}
	p.mu.Lock()
	if len(p.cache) >= 512 {
		p.cache = map[string]prices.Quote{}
	}
	p.cache[id] = q
	p.mu.Unlock()
	return q, nil
}

var _ prices.Provider = (*Provider)(nil)
