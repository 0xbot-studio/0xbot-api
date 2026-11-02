package llamaprices

import (
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/prices"
	"context"
	"os"
	"testing"
	"time"
)

// Explicit opt-in: no account creation, credentials or paid endpoint.
func TestLivePrices(t *testing.T) {
	if os.Getenv("OXBOT_LIVE_PRICES") != "1" {
		t.Skip("set OXBOT_LIVE_PRICES=1 for public upstream reads")
	}
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for _, asset := range []string{"BTC", "ETH", "BNB"} {
		q, e := p.AssetPrice(ctx, prices.AssetRequest{Asset: asset})
		if e != nil || q.Asset != asset || q.Currency != "USD" || q.Price == "" {
			t.Fatalf("%s: %v", asset, e)
		}
		t.Logf("%s reference quote verified (source=%s)", asset, q.Source)
	}
	for _, token := range data.Tokens() {
		q, e := p.TokenPrice(ctx, prices.TokenRequest{Network: token.Network, Contract: token.Contract})
		if e != nil || q.Network != token.Network || q.Contract != token.Contract || q.Price == "" {
			t.Fatalf("%s/%s: %v", token.Network, token.Symbol, e)
		}
		t.Logf("%s %s contract quote verified", token.Network, token.Symbol)
	}
}
