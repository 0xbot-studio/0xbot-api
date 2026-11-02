package httpapi

import (
	"0xbot.org/api/internal/prices"
	"context"
	"time"
)

type priceFixture struct{}

func (*priceFixture) ID() string             { return "defillama-prices" }
func (*priceFixture) Supports(n string) bool { return n == "market" || n == "eip155:1" }
func (*priceFixture) CapabilityIDs() []string {
	return []string{"market.asset_price", "market.token_price"}
}
func (*priceFixture) AssetPrice(_ context.Context, in prices.AssetRequest) (prices.Quote, error) {
	return prices.Quote{Asset: in.Asset, Price: "123.4567890123456789", Currency: "USD", Symbol: in.Asset, Source: "fixture", UpdatedAt: time.Now().UTC(), FetchedAt: time.Now().UTC()}, nil
}
func (*priceFixture) TokenPrice(_ context.Context, in prices.TokenRequest) (prices.Quote, error) {
	return prices.Quote{Network: in.Network, Contract: in.Contract, Price: "0.999", Currency: "USD", Source: "fixture", UpdatedAt: time.Now().UTC(), FetchedAt: time.Now().UTC()}, nil
}
