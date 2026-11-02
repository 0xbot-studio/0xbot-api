package llamaprices

import (
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/prices"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDecimal(t *testing.T) {
	for in, want := range map[string]string{"79345.20141891086": "79345.20141891086", "1e-18": "0.000000000000000001", "1.000": "1", "1e+4": "10000", "9007199254740993": "9007199254740993"} {
		got, e := decimal(in)
		if e != nil || got != want {
			t.Fatal(in, got, e)
		}
	}
	for _, s := range []string{"0", "-1", "NaN", "Infinity", "01", "1/2", "1e999999999", "1e-100", "0.00000000000000000000000000000000000000001"} {
		if _, e := decimal(s); e == nil {
			t.Fatal(s)
		}
	}
}
func TestQuotes(t *testing.T) {
	for _, tc := range []struct {
		name, price, confidence string
		age                     time.Duration
		status                  int
		missing                 bool
		want                    error
	}{
		{"valid", "1e-8", "0.99", time.Second, 200, false, nil},
		{"missing", "1", "1", time.Second, 200, true, prices.ErrNotFound},
		{"stale", "1", "1", 11 * time.Minute, 200, false, prices.ErrStale},
		{"future", "1", "1", -2 * time.Minute, 200, false, data.ErrUnavailable},
		{"low", "1", "0.8", time.Second, 200, false, prices.ErrConfidence},
		{"bad price", "0", "1", time.Second, 200, false, data.ErrUnavailable},
		{"rate", "1", "1", time.Second, 429, false, data.ErrRateLimited},
		{"failure", "1", "1", time.Second, 503, false, data.ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(tc.status)
				coins := map[string]any{}
				if !tc.missing {
					coins["coingecko:bitcoin"] = map[string]any{"price": json.Number(tc.price), "confidence": json.Number(tc.confidence), "symbol": "BTC", "timestamp": time.Now().Add(-tc.age).Unix()}
				}
				json.NewEncoder(w).Encode(map[string]any{"coins": coins})
			}))
			defer ts.Close()
			p := newProvider(ts.URL, ts.Client())
			q, e := p.AssetPrice(context.Background(), prices.AssetRequest{Asset: "btc"})
			if !errors.Is(e, tc.want) {
				t.Fatal(q, e)
			}
			if e != nil {
				if q.Price != "" {
					t.Fatal("partial price")
				}
				return
			}
			q2, e := p.AssetPrice(context.Background(), prices.AssetRequest{Asset: "BTC"})
			if e != nil || !q2.Cached || q.Cached || calls != 1 || q.Price != "0.00000001" || q2.UpdatedAt != q.UpdatedAt || q.Asset != "BTC" || q.Contract != "" {
				t.Fatal(q, q2, e, calls)
			}
		})
	}
}
func TestContractMappingAndBudget(t *testing.T) {
	for network, chain := range map[string]string{"eip155:1": "ethereum", "eip155:56": "bsc", "eip155:137": "polygon"} {
		contract := "0x" + strings.Repeat("a", 40)
		id := chain + ":" + contract
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/prices/current/"+id {
				t.Error(r.URL.Path)
			}
			json.NewEncoder(w).Encode(map[string]any{"coins": map[string]any{id: map[string]any{"price": json.Number("0.9999347132548848"), "confidence": 1, "timestamp": time.Now().Unix(), "symbol": "USDC"}}})
		}))
		p := newProvider(ts.URL, ts.Client())
		q, e := p.TokenPrice(context.Background(), prices.TokenRequest{Network: network, Contract: contract})
		if e != nil || q.Contract != contract || q.Network != network || q.Currency != "USD" || q.Price != "0.9999347132548848" {
			t.Fatal(q, e)
		}
		p.mu.Lock()
		p.calls = 60
		p.window = time.Now()
		p.mu.Unlock()
		_, e = p.AssetPrice(context.Background(), prices.AssetRequest{Asset: "ETH"})
		if !errors.Is(e, data.ErrRateLimited) {
			t.Fatal(e)
		}
		ts.Close()
	}
}
func TestMalformed(t *testing.T) {
	for _, raw := range []string{"null", "{}", `{"coins":null}`, strings.Repeat(" ", (64<<10)+1), `{"coins":{"coingecko:bitcoin":{"price":1e9999999,"confidence":1,"timestamp":1}}}`} {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(raw)) }))
		p := newProvider(ts.URL, ts.Client())
		if q, e := p.AssetPrice(context.Background(), prices.AssetRequest{Asset: "BTC"}); e == nil || q.Price != "" {
			t.Fatal("malformed accepted")
		}
		ts.Close()
	}
}

func TestCancelledCachedQuote(t *testing.T) {
	p := New()
	p.cache["coingecko:bitcoin"] = prices.Quote{Price: "1", FetchedAt: time.Now(), UpdatedAt: time.Now()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	q, err := p.AssetPrice(ctx, prices.AssetRequest{Asset: "BTC"})
	if !errors.Is(err, context.Canceled) || q.Price != "" {
		t.Fatalf("cancelled request returned quote: %v, %v", q, err)
	}
}
