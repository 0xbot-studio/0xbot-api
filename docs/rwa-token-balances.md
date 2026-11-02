# Tokenized-equity read capabilities (RWA)

API 0.11.0 adds curated tokenized-equity (xStocks) reads: one balance capability and one reference-price capability. Both use `data:read`; REST uses existing API keys/sessions, MCP also accepts the existing MCP OAuth grant. No issuance, trading, custody, transfer or signature is involved.

| Capability | MCP | REST POST |
| --- | --- | --- |
| `data.rwa.token_balance` | `get_rwa_token_balance` | `/v1/data/rwa/token-balance` |
| `market.equity_price` | `get_equity_price` | `/v1/prices/equity` |

`get_supported_tokens` / `GET /v1/tokens` now returns `asset_class`, `underlying` and `issuer` per entry. `asset_class=tokenized_equity` entries are accepted only by the two capabilities above; `get_token_balance` and `get_common_balances` continue to accept stablecoins only, and the common snapshot stays a fixed three assets.

## Curated contracts (first release)

Ethereum `eip155:1` only. Issuer: Backed Assets (xStocks, 1:1 tokenized exposure to custodian-held shares; not available to US persons).

| Contract | Symbol | Underlying | Decimals |
| --- | --- | --- | --- |
| `0x9d275685dc284c8eb1c79f6aba7a63dc75ec890a` | AAPLx | AAPL | 18 |
| `0xcf2810d523c2efb7e4d3afa2629e635d2032ddb4` | TSLAX | TSLA | 18 |
| `0xe9fa01197b7e836982a1cab03fe591f7e331e313` | AMATx | AMAT | 18 |

Provenance: addresses cross-checked between Etherscan and Ethplorer listings (2026-11-02) and restricted to this three-entry set; INTCx was excluded because two distinct upstream equity feeds exist with no authoritative tiebreaker. On-chain verification via PublicNode on 2026-11-03 confirmed `symbol()`, `name()`, `decimals()=18` and deployed code for all three contracts (on-chain `name()` values are mirrored in the registry).

## Balance

Input: `address`, `network`, exact `contract` from the curated registry. Same-block `decimals()` verification and `balanceOf()`, one provider, one block — the same read discipline as `get_token_balance`. Zero is a valid result.

A balance is an on-chain observation. It is not holding eligibility, a compliance verdict, or a tradable quote; the issuer's availability restrictions (including the non-US limitation) still apply to what the balance represents.

## Equity reference price

Input: `network` plus exact curated `contract`. The price describes the **underlying stock** (AAPL/TSLA/AMAT), not the token. Served by the `pyth-prices` provider (Pyth Hermes public pull API, no credentials), whose curated feed mapping is provider-internal eligibility:

- Exact positive decimal string scaled from Pyth integer price with negative exponent; bounds enforced (exponent ≥ −40, magnitude < 10^14).
- `confidence` reports price/(price+conf) as a 0..1 score; below 0.9 is `price_low_confidence`.
- Observations older than 10 minutes are `price_stale` (equities go stale when the market is closed); last-known prices are never returned.
- Hermes returning 401/403/5xx or being unreachable maps to `provider_unavailable`; the provider fails closed and the dashboard can disable the route.

Not an executable quote, NAV, or valuation.

## Limits and errors

- `data.rwa.token_balance`: 20 calls/minute/workspace (`rate_limited`, `Retry-After: 60`).
- `market.equity_price`: shares the existing 20/minute/workspace price budget with asset/token prices.
- 400 `invalid_request` (non-curated contract, wrong network), 401 missing/revoked auth, 429 rate limited, 503 provider unavailable; price errors add `price_stale` / `price_low_confidence` / `price_not_found`. MCP returns tool errors without partial success data.
- Automatic routing may retry the complete read on another eligible provider in auto mode; explicit provider selection never silently falls back.

## Boundaries

- No transfers, approvals, signing, mint/redeem, eligibility lookups, corporate actions, NAV, or cross-chain reads. Those need separate capability contracts and data sources (see ADR 0015 follow-ups).
- BSC xStocks and Ondo Stocks entries are deliberately absent pending verified addresses and review of their permissioned transfer models.
- The stablecoin balance capabilities are unchanged: registry entries with `asset_class=tokenized_equity` are filtered out of `get_common_balances` and rejected by `get_token_balance`.

## Verification

Offline: registry separation and class-shape tests; evmrpc same-block/decimals verification and three-asset snapshot exclusion tests; routing capability eligibility (undeclared providers never receive RWA calls) and fallback; per-workspace limiter and shared price budget; REST/MCP contract parity against OpenAPI 0.11.0; Pyth adapter fixture tests for exact decimals, confidence derivation, staleness, 401/429 mapping and upstream rate cap. Full `go vet`, `go build` and `go test -race ./...` pass.

Online: read-only `symbol()/name()/decimals()/eth_getCode` calls against PublicNode verified all three curated contracts on 2026-11-03; no account, key or transaction was involved. Still open at deployment time: Hermes `/v2/updates/price/latest` reachability from the release host — it returned 401 from the development network on 2026-11-02/03 while the catalog endpoint works; the price provider fails closed until verified.

Reference: [ADR 0015](https://github.com/0xbot-studio/0xbot/blob/main/docs/adr/0015-rwa-equity-reads.md), [xStocks](https://xstocks.fi), [Pyth Hermes](https://docs.pyth.network).
