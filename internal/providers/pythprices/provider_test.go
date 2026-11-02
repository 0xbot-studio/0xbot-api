package pythprices

import (
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/prices"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const aaplFeed = "49f6b65cb1de6b10eaf75e7c03ca029c306d0357e91b5311b175084a5ad55688"

func aaplContract(t *testing.T) string {
	t.Helper()
	token, ok := data.FindEquityToken("eip155:1", "0x9d275685dc284c8eb1c79f6aba7a63dc75ec890a")
	if !ok || token.Underlying != "AAPL" {
		t.Fatal("AAPL equity entry missing from curated registry")
	}
	return token.Contract
}

func fixture(publish int64) string {
	return fmt.Sprintf(`{"parsed":[{"id":%q,"price_feed":{"id":%q,"price":"319456000000","conf":"200000000","expo":-8,"publish_time":%d}}]}`, aaplFeed, aaplFeed, publish)
}

func TestEquityPriceParsesExactDecimalAndConfidence(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, aaplFeed) {
			t.Errorf("unexpected feed query %s", r.URL.RawQuery)
		}
		w.Write([]byte(fixture(time.Now().Unix())))
	}))
	defer s.Close()
	p := newProvider(s.URL, s.Client())
	in := prices.EquityRequest{Network: "eip155:1", Contract: aaplContract(t)}
	q, e := p.EquityPrice(context.Background(), in)
	if e != nil || q.Price != "3194.56" || q.Source != "Pyth" || q.Currency != "USD" || q.Symbol != "AAPLx" || q.Network != "eip155:1" || q.SourceAsset == "" {
		t.Fatalf("bad quote: %+v err=%v", q, e)
	}
	if q.UpdatedAt.After(time.Now().UTC().Add(time.Minute)) {
		t.Fatal("future publish time accepted")
	}
	// Cached second call must not hit the fixture server again.
	q2, e := p.EquityPrice(context.Background(), in)
	if e != nil || !q2.Cached || q2.Price != "3194.56" {
		t.Fatalf("cache miss on second read: %+v err=%v", q2, e)
	}
}

func TestEquityPriceMapsUpstreamFailures(t *testing.T) {
	now := time.Now().Unix()
	cases := []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"unauthorized", 401, "unauthorized", data.ErrUnavailable},
		{"forbidden", 403, "forbidden", data.ErrUnavailable},
		{"rate limited", 429, "", data.ErrRateLimited},
		{"stale", 200, fixture(now - 30*60), prices.ErrStale},
		{"low confidence", 200, `{"parsed":[{"id":"` + aaplFeed + `","price_feed":{"id":"` + aaplFeed + `","price":"100000000000","conf":"50000000000","expo":-8,"publish_time":` + fmt.Sprint(now) + `}}]}`, prices.ErrConfidence},
		{"empty payload", 200, `{"parsed":[]}`, data.ErrUnavailable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(c.status)
				w.Write([]byte(c.body))
			}))
			defer s.Close()
			p := newProvider(s.URL, s.Client())
			_, e := p.EquityPrice(context.Background(), prices.EquityRequest{Network: "eip155:1", Contract: aaplContract(t)})
			if e != c.want {
				t.Fatalf("want %v got %v", c.want, e)
			}
		})
	}
}

func TestEquityPriceRejectsNonCuratedContracts(t *testing.T) {
	p := newProvider("http://unused.invalid", nil)
	if _, e := p.EquityPrice(context.Background(), prices.EquityRequest{Network: "eip155:1", Contract: "0xdac17f958d2ee523a2206206994597c13d831ec7"}); e != data.ErrInput {
		t.Fatal("stablecoin accepted by equity price")
	}
	if _, e := p.AssetPrice(context.Background(), prices.AssetRequest{Asset: "BTC"}); e != prices.ErrNotFound {
		t.Fatal("asset price must stay unsupported")
	}
	if _, e := p.TokenPrice(context.Background(), prices.TokenRequest{Network: "eip155:1", Contract: aaplContract(t)}); e != prices.ErrNotFound {
		t.Fatal("token price must stay unsupported")
	}
}

func TestScaledBoundaries(t *testing.T) {
	if price, conf, e := scaled("319456000000", "200000000", -8); e != nil || price != "3194.56" || conf != "2" {
		t.Fatalf("scaled -8: %s %s %v", price, conf, e)
	}
	if price, _, e := scaled("123456", "1", -6); e != nil || price != "0.123456" {
		t.Fatalf("scaled -6: %s %v", price, e)
	}
	if _, _, e := scaled("0", "1", -8); e != data.ErrUnavailable {
		t.Fatal("zero price accepted")
	}
	if _, _, e := scaled("1", "1", 0); e != data.ErrUnavailable {
		t.Fatal("positive exponent accepted")
	}
	if _, _, e := scaled("1", "1", -41); e != data.ErrUnavailable {
		t.Fatal("exponent bound not enforced")
	}
}

func TestUpstreamRateCap(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("ids[]")
		w.Write([]byte(fmt.Sprintf(`{"parsed":[{"id":%q,"price_feed":{"id":%q,"price":"319456000000","conf":"200000000","expo":-8,"publish_time":%d}}]}`, id, id, time.Now().Unix())))
	}))
	defer s.Close()
	p := newProvider(s.URL, s.Client())
	for i := 0; i < 60; i++ {
		if _, e := p.fetch(context.Background(), feed{id: fmt.Sprintf("feed-%d", i)}); e != nil {
			t.Fatalf("call %d unexpectedly failed: %v", i, e)
		}
	}
	if _, e := p.fetch(context.Background(), feed{id: "over"}); e != data.ErrRateLimited {
		t.Fatalf("rate cap not enforced: %v", e)
	}
}
