# Reference prices · API 0.7.0

Read-only USD reference prices, not executable quotes, guaranteed liquidity, token safety assessments or exchange rates to USDT. No wallet signing or transaction execution. Prices are per whole token and must not be scaled by token decimals.

| Capability | MCP tool | REST POST |
| --- | --- | --- |
| market.asset_price | get_asset_price | /v1/prices/asset |
| market.token_price | get_token_price | /v1/prices/token |

Asset input: `{"asset":"BTC"}` (BTC, ETH or BNB). This is a global native-asset reference, not a chain balance; WBTC is not BTC.

Contract input: `{"network":"eip155:56","contract":"0x55d398326f99059ff775485246999027b3197955"}`. Supported networks: Ethereum eip155:1, BSC eip155:56, Polygon eip155:137. Require the exact nonzero contract and network; never resolve by ticker. Availability depends on upstream coverage, not every deployed token has a price. Both inputs accept optional `provider`, default `auto`.

Both use the existing QueryResult envelope and a typed data object: asset, network, contract, symbol, currency, price, source, source_asset, confidence, updated_at, fetched_at, cached. Global asset quotes have empty network/contract; contract quotes have empty asset. Symbol is informational, not identity. Price and confidence are decimal strings. Updated_at is the source observation time, fetched_at the gateway retrieval time; cached hits preserve both. Provider and source provenance remain visible in results even though the public provider directory is hidden.

REST requires existing session or API key; MCP accepts the existing read-only OAuth flow or API key. REST and MCP share a PostgreSQL-backed limit of 20 price queries per workspace per minute, routing, permission checks and usage records.

## Initial adapter

Provider `defillama-prices` uses `https://coins.llama.fi/prices/current/{coins}` with no configured account or API key. Mapping: BTC → coingecko:bitcoin, ETH → coingecko:ethereum, BNB → coingecko:binancecoin. Contracts use ethereum:, bsc: or polygon: plus the lowercased exact address. See [official endpoint reference](https://api-docs.defillama.com/llms-free.txt). Upstream identifiers do not imply direct integration with another provider.

Cache: 30 seconds, at most 512 entries, process-local; bounded upstream budget of 60 cold fetches/minute/process. This is our protective limit, not a statement of the upstream quota. Multiple replicas have independent caches and budgets. HTTP timeout 6 seconds, route attempt timeout 7 seconds, redirects disabled, maximum response 64 KiB.

Reject prices older than 10 minutes, timestamps more than 60 seconds in the future, and confidence below 0.9 or above 1. Exact positive decimal values only, between 1e-40 and 1e40, at most 40 fractional digits without rounding; exponent parsing is bounded before allocation. Confidence is an upstream score, not an independent guarantee. Failed or expired observations are never replaced by zero or silently served stale.

Errors: 404 price_not_found; 422 price_stale / price_low_confidence; 429 rate_limited; 503 provider_unavailable. Existing invalid-input and routing errors retain their standard mappings. MCP marks failed tool results as errors. Missing/stale/low-confidence prices do not trip the provider circuit breaker or silently switch source.

Migration 004 adds the provider route idempotently and does not change existing routes. Back up before deployment. Public API availability does not establish commercial redistribution rights or an SLA; verify the applicable terms before monetizing redistribution at scale.

## Verification

Default tests use fixtures, including precision, invalid data, cancellation, cache, routing, limits and authenticated REST/MCP parity. Opt-in upstream check: `OXBOT_LIVE_PRICES=1 go test -v -run TestLivePrices -count=1 ./internal/providers/llamaprices`. It checks BTC/ETH/BNB and all six curated stablecoin contracts. A live pass confirms that observation only, not future availability.
