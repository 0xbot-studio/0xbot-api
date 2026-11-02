# 0xbot API

Go provider aggregation gateway. A modular monolith with REST and Streamable HTTP MCP sharing balance and read-only token-bound-account use cases.

## Implemented preview

- API 0.9.0 adds PADIN Rooms public roundtables: ten REST/MCP capabilities, explicit rooms scopes, host-assigned turn order, audience questions, bounded event waits, extractive recap and host-submitted cited summary. Twenty-three capabilities, twenty-four tools; ten-room/day creation cap. A separate opt-in guest Runner accepts a trusted model driver; no model is automatically configured or billed. Deployed in 20260908-preview-15. See [room guide](docs/rooms.md) and migration 006.

- API 0.8.0 adds PADIN Inbox: send_message, list_messages and get_message, wallet-bound private-message OAuth scopes, separate bilingual UI and idempotent storage. Thirteen capabilities, fourteen MCP tools. Deployed in 20260908-preview-14; no XMTP or E2EE. See [message guide](docs/messages.md) and migration 005.
- API 0.7.0 adds get_asset_price and get_token_price for BTC/ETH/BNB and exact contracts on Ethereum/BSC/Polygon (ten capabilities, eleven tools). Dedicated provider adapter, exact USD reference-price strings, freshness checks and bounded cache. See [price capability guide](docs/prices.md). Migration 004 adds the price route; deployed in 20260908-preview-13.
- EIP-4361 login challenge + EIP-191 EOA signature verification; atomic nonce consumption and account registration.
- X authorization-code + PKCE adapter, browser-bound one-use state, profile identity mapping and explicit account linking. Requires your X application credentials for live activation.
- Server-side sessions, separate admin audience, admin wallet allowlist and TOTP step-up.
- Personal workspace, hashed read-only API keys, revocation, PostgreSQL rate limits and request/usage history.
- Native ETH, BNB and POL plus six curated USDT/USDC contracts across Ethereum, BSC and Polygon; chain and decimals validation, pinned block, exact base-unit strings, timeout, eligible-provider fallback and circuit breaker.
- GET /v1/tokens lists exact contracts and variants; POST /v1/data/token-balance queries one; POST /v1/data/balances queries native asset plus both curated tokens at one block. Batch queries have a shared REST/MCP limit of 20 per workspace per minute and return no partial success.
- MCP balance tools share the same domain registry and use cases. API 0.6.0 adds get_nft_detail, get_nft_owner and get_nft_balance alongside resolve_token_bound_account and get_token_bound_account (eight capabilities, nine tools). No payment or transaction execution. See [NFT capability guide](docs/nft-queries.md) and [account capability guide](docs/token-bound-accounts.md) for explicit inputs and safety boundaries. Wallet enumeration and complete transfer history are not implemented.

## Local setup

MCP OAuth is implemented locally alongside API keys: public-client dynamic registration, PKCE S256, browser consent through the user app, 15-minute access tokens, rotating refresh tokens and 30-day grants. Discovery is at `/.well-known/oauth-protected-resource/mcp` and `/.well-known/oauth-authorization-server`. OAuth tokens are restricted to `/mcp`; REST continues to use existing sessions/API keys. Default and existing grants remain `data:read`. Optional `messages:read` / `messages:send` require explicit wallet-bound consent; private-message REST uses app sessions only. CIMD and confidential clients are not implemented. See root ADR 0005 for protocol constraints, replay behavior, cleanup and rollout. No extra server signing key is required for opaque hashed tokens.

Run the API and app with matching API_ORIGIN/APP_ORIGIN, use COOKIE_SECURE=false only for local HTTP. The public website's setup examples target production domains; they become usable only after coordinated deployment. X login still needs X application credentials. New migration 003 is additive; run backups before deploying it.

Requires Go 1.26.8+ and PostgreSQL. Create a dedicated oxbot_dev database. Copy .env.example to a private local environment file and replace placeholders; source it in your shell (the server does not implicitly load dotenv). Then run make run. Frontend origin defaults must match the exact browser origin; app and dashboard use ports 3191 and 3192 in the provided local script.

Database migrations in internal/platform/postgres/migrations run transactionally under an advisory lock during startup. Existing migration files are immutable. See the root ADR for the deliberate first-release pgx/embedded-migration implementation in place of the earlier sqlc/goose candidates.

## Checks

- make fmt, make lint, make test, make build.
- TEST_DATABASE_URL='postgres://USER@localhost/oxbot_test?sslmode=disable' go test -race ./...
- go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...

The integration test requires a dedicated database ending in _test. It creates isolated random test users and does not truncate existing tables. Real provider smoke: go run ./cmd/smoke -api http://localhost:8095 -app http://localhost:3191. This creates an unfunded test identity and a temporary API key, performs read-only queries, and revokes the key/session; its test account/history remain as audit evidence.

## Boundaries

Capability contract v1: `internal/capabilities` owns IDs, versions, rules and REST/MCP mappings. Routed read results share `data.QueryResult[T]` without changing existing JSON. Adapters declare supported capability IDs. `GET /v1/admin/contracts` exposes the code-owned registry, OpenAPI schemas, actual MCP registration schemas and current provider settings; administrator session and TOTP are required. No online contract editing or credentials are exposed. See root ADR 0006. Contract parity and admin/MFA isolation are tested alongside the existing provider fixtures.

- internal/data: capability types and input validation.
- internal/tokenbound: optional ERC-6551 read contracts and input validation; no creation or transaction authorization.
- internal/providers/evmrpc: external HTTP/JSON-RPC normalization.
- internal/routing: eligibility, priority and circuit state; settings own their audit records.
- internal/operations: execution use case and idempotent metering records.
- internal/identity and internal/apikeys: external identities, sessions, credentials.
- internal/httpapi and internal/mcpserver: transport adapters, no duplicate provider business logic.
- internal/contract/openapi.json: published REST contract. Frontend clients are generated, not hand-maintained.

Only EVM EOAs are supported for first-release login; smart-contract-wallet signature validation and non-EVM wallets are future capabilities. No email/password or automatic wallet custody. Admin MFA currently uses one operator secret. Multiple workspaces, memberships, identity unlink/recovery and paid billing are not yet implemented.

Engineering policy: root AGENTS.md, baseline 2026-09-07, is mirrored in docs/engineering-policy.md so this repository can be cloned independently.

### LI.FI swaps (EVM preview)

Set `LIFI_API_KEY` in the server runtime secret environment, or explicitly set
`LIFI_PUBLIC_ACCESS=true` to use LI.FI’s unauthenticated public quota. Optionally set
`LIFI_INTEGRATOR` (default `0xbot`). Never put the key in frontend variables.
The additive `008_lifi.sql` migration registers the route without overwriting
operator settings. Disable the `lifi` provider in dashboard to stop new reads.
With neither a key nor public opt-in, swaps fail closed. No server wallet or transaction broadcast exists.

REST `POST /v1/swaps/tokens`, `/quote`, `/status` map to MCP
`get_swap_tokens`, `get_swap_quote`, `get_swap_status`; all are read-only under
`data:read`. Supported networks: Ethereum, Optimism, BSC, Polygon, Base, Arbitrum.
`from_token`/`to_token` use exact addresses; the zero address means native currency.
Amounts are uint256 base-unit strings. Source and recipient are the same explicit
wallet. Slippage is 10–100 integer basis points. Tokens outside LI.FI coverage or
pairs without a usable route return an error. Quote preparation is not authorization.

Queries share 30/minute/workspace, the existing 60/minute overall limit, and an
upstream cap of 90/minute with 8 simultaneous calls. Public mode also bounds
quotes to 70 per rolling two hours per process (below LI.FI’s 75/IP limit);
upstream 429/reset headers remain authoritative across restarts and shared IPs.
Quote cooldown does not block status reads. Token catalogs cache for 5
minutes; quotes never cache and expire after 60 seconds. Calls time out after 18
seconds; no automatic retry or provider fallback. Costs retain token denomination
and inclusion flags. `fee_usd=0` is the platform query fee, not free swaps.

`completed` requires LI.FI destination completion; `NOT_FOUND`, partial/refunded
results and unknown status never become success or trigger resubmission. Upstream
errors are sanitized. Every query records an operation, without wallet/calldata
in general operation logs. Transaction execution/history remains wallet-owned.

Regenerate the swap contract with `go run ./cmd/sync-swap-contracts > /tmp/swaps-openapi.json`,
then replace `internal/contract/openapi.json` and regenerate clients from that file.
Validate: `TEST_DATABASE_URL='postgres:///oxbot_swaps_test?sslmode=disable' go test -race ./...`,
`go vet ./...`, `go build ./...`. The test database must be dedicated and end in `_test`.

Deployment smoke: `go run ./cmd/smoke -api https://api.0xbot.org -app https://app.0xbot.org -swaps-only` checks synthetic login, catalogs, REST/MCP quotes and key revocation without submitting a transaction.

## License

Apache-2.0, see [LICENSE](LICENSE). The 0xbot name, logo and 0xbot.org domains are project trademarks and are not licensed for third-party deployments — please rebrand self-hosted forks. Platform overview, architecture and ADRs: [0xbot-studio/0xbot](https://github.com/0xbot-studio/0xbot).
