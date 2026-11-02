// Package pythprices adapts public Pyth Hermes equity reference prices
// without API credentials. The upstream may reject unauthenticated datacenter
// traffic; every failure maps to an explicit error and never to fabricated data.
package pythprices

import (
	"0xbot.org/api/internal/capabilities"
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/prices"
	"context"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// feed maps a curated tokenized-equity contract to its Pyth equity price feed.
// Feed IDs were read from the public Hermes /v2/price_feeds catalog on
// 2026-11-02 (asset_type=Equity); contracts mirror docs/adr/0015.
type feed struct {
	id     string
	symbol string
}

var feeds = map[string]feed{
	"0x9d275685dc284c8eb1c79f6aba7a63dc75ec890a": {"49f6b65cb1de6b10eaf75e7c03ca029c306d0357e91b5311b175084a5ad55688", "AAPL"},
	"0xcf2810d523c2efb7e4d3afa2629e635d2032ddb4": {"16dad506d7db8da01c87581c87ca897a012a153557d4d578c3b9c9e1bc0632f1", "TSLA"},
	"0xe9fa01197b7e836982a1cab03fe591f7e331e313": {"b9bc74cc1243b706efacf664ed206d08ab1dda79e8b87752c7c44b3bdf1b9e08", "AMAT"},
}

type Provider struct {
	client *http.Client
	base   string
	mu     sync.Mutex
	cache  map[string]prices.Quote
	window time.Time
	calls  int
}

func New() *Provider {
	return newProvider("https://hermes.pyth.network", &http.Client{Timeout: 6 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }})
}
func newProvider(base string, client *http.Client) *Provider {
	return &Provider{base: base, client: client, cache: map[string]prices.Quote{}}
}
func (p *Provider) ID() string { return "pyth-prices" }
func (p *Provider) Supports(n string) bool {
	return n == "eip155:1"
}
func (p *Provider) CapabilityIDs() []string {
	return []string{capabilities.EquityPrice}
}

// AssetPrice and TokenPrice are intentionally unsupported: this adapter serves
// only the curated tokenized-equity subset behind market.equity_price.
func (p *Provider) AssetPrice(context.Context, prices.AssetRequest) (prices.Quote, error) {
	return prices.Quote{}, prices.ErrNotFound
}
func (p *Provider) TokenPrice(context.Context, prices.TokenRequest) (prices.Quote, error) {
	return prices.Quote{}, prices.ErrNotFound
}

func (p *Provider) EquityPrice(ctx context.Context, in prices.EquityRequest) (prices.Quote, error) {
	if err := in.Validate(); err != nil {
		return prices.Quote{}, err
	}
	token, _ := data.FindEquityToken(in.Network, in.Contract)
	f, ok := feeds[strings.ToLower(token.Contract)]
	if !ok {
		return prices.Quote{}, prices.ErrNotFound
	}
	q, err := p.fetch(ctx, f)
	if err != nil {
		return prices.Quote{}, err
	}
	q.Network, q.Contract, q.Symbol = token.Network, token.Contract, token.Symbol
	return q, nil
}

func (p *Provider) fetch(ctx context.Context, f feed) (prices.Quote, error) {
	if err := ctx.Err(); err != nil {
		return prices.Quote{}, err
	}
	now := time.Now().UTC()
	p.mu.Lock()
	if q, ok := p.cache[f.id]; ok && now.Sub(q.FetchedAt) < 30*time.Second {
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
	req, err := http.NewRequestWithContext(ctx, "GET", p.base+"/v2/updates/price/latest?ids%5B%5D="+f.id, nil)
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
		Parsed []struct {
			ID        string `json:"id"`
			PriceFeed struct {
				ID          string `json:"id"`
				Price       string `json:"price"`
				Conf        string `json:"conf"`
				Expo        int    `json:"expo"`
				PublishTime int64  `json:"publish_time"`
			} `json:"price_feed"`
		} `json:"parsed"`
	}
	if json.Unmarshal(raw, &payload) != nil || len(payload.Parsed) == 0 {
		return prices.Quote{}, data.ErrUnavailable
	}
	item := payload.Parsed[0]
	if item.ID != f.id || item.PriceFeed.ID != f.id {
		return prices.Quote{}, data.ErrUnavailable
	}
	price, conf, err := scaled(item.PriceFeed.Price, item.PriceFeed.Conf, item.PriceFeed.Expo)
	if err != nil {
		return prices.Quote{}, err
	}
	now = time.Now().UTC()
	updated := time.Unix(item.PriceFeed.PublishTime, 0).UTC()
	if item.PriceFeed.PublishTime <= 0 || updated.After(now.Add(time.Minute)) {
		return prices.Quote{}, data.ErrUnavailable
	}
	if now.Sub(updated) > 10*time.Minute {
		return prices.Quote{}, prices.ErrStale
	}
	numerator, nok := new(big.Rat).SetString(price)
	confRat, cok := new(big.Rat).SetString(conf)
	if !nok || !cok {
		return prices.Quote{}, data.ErrUnavailable
	}
	// Report a defillama-compatible 0..1 confidence score derived from the
	// Pyth conf interval: price / (price + conf).
	score := new(big.Rat).Quo(numerator, new(big.Rat).Add(numerator, confRat))
	if score.Cmp(big.NewRat(9, 10)) < 0 || score.Cmp(big.NewRat(1, 1)) > 0 {
		return prices.Quote{}, prices.ErrConfidence
	}
	shortID := f.id
	if len(shortID) > 16 {
		shortID = shortID[:16]
	}
	q := prices.Quote{Price: price, Currency: "USD", Source: "Pyth", SourceAsset: "pyth:equity:" + f.symbol + ":" + shortID, Confidence: strings.TrimRight(strings.TrimRight(score.FloatString(12), "0"), "."), UpdatedAt: updated, FetchedAt: now}
	p.mu.Lock()
	if len(p.cache) >= 512 {
		p.cache = map[string]prices.Quote{}
	}
	p.cache[f.id] = q
	p.mu.Unlock()
	return q, nil
}

// scaled converts Pyth integer price/conf with negative exponent into exact
// decimal strings, rejecting zero prices and unreasonable magnitudes.
func scaled(price, conf string, expo int) (string, string, error) {
	pn, ok := new(big.Int).SetString(price, 10)
	if !ok || pn.Sign() <= 0 || pn.BitLen() > 256 {
		return "", "", data.ErrUnavailable
	}
	cn, ok := new(big.Int).SetString(conf, 10)
	if !ok || cn.Sign() < 0 || cn.BitLen() > 256 {
		return "", "", data.ErrUnavailable
	}
	if expo >= 0 || expo < -40 {
		return "", "", data.ErrUnavailable
	}
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(-expo)), nil)
	rat := new(big.Rat).SetFrac(pn, scale)
	crat := new(big.Rat).SetFrac(cn, scale)
	if rat.Cmp(new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(14), nil))) > 0 {
		return "", "", data.ErrUnavailable
	}
	value := strings.TrimRight(strings.TrimRight(rat.FloatString(40), "0"), ".")
	check, valid := new(big.Rat).SetString(value)
	if !valid || check.Cmp(rat) != 0 {
		return "", "", data.ErrUnavailable
	}
	cvalue := strings.TrimRight(strings.TrimRight(crat.FloatString(40), "0"), ".")
	if cvalue == "" {
		cvalue = "0"
	}
	return value, cvalue, nil
}

var _ prices.Provider = (*Provider)(nil)
