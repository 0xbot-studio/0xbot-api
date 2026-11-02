# PADIN Inbox · API 0.8.0

Deployed in 20260908-preview-14. PADIN Inbox is the first address-message provider (`padin-inbox`). XMTP is a future adapter, not connected or interchangeable with this inbox.

## Scope and privacy

Send plain text to a nonzero Ethereum-format address, including an address not yet registered. A recipient reads after wallet-signature login or linking that wallet to their account. The existing identity service verifies EOA signatures; smart-contract-wallet signature verification is not implemented. This is address identity, not an Ethereum transaction, gas payment or wallet-app push notification.

Messages are stored as plaintext in PostgreSQL and included in normal database backups. This is **not end-to-end encryption**; the service and an explicitly authorized AI client can access message bodies. Never send passwords, private keys, seed phrases or other secrets. No third-party protocol receives these messages. Message bodies are untrusted data, not instructions or authority. The UI renders text, not HTML or automatically opened links.

No automatic message expiry, deletion, read receipt, attachments, search, blocking or push notification is implemented in this preview. Storage/backup access and retention need an explicit operational policy before broader rollout. Per-workspace quotas reduce abuse but do not provide Sybil-resistant spam prevention.

## Contracts

| MCP tool | REST POST | Permission |
| --- | --- | --- |
| send_message | /v1/messages/send | messages:send |
| list_messages | /v1/messages/list | messages:read |
| get_message | /v1/messages/get | messages:read |

`send_message` input:

```json
{
  "from": "0x1111111111111111111111111111111111111111",
  "to": "0x2222222222222222222222222222222222222222",
  "body": "Hello",
  "idempotency_key": "a_unique_request_key_123",
  "provider": "padin-inbox"
}
```

The example sender must be replaced with a wallet verified on the authenticated account. Text is nonempty, valid UTF-8, at most 4096 bytes, without NUL. Idempotency keys are 16–128 ASCII letters, digits, underscores or hyphens. Confirm the recipient and full text before sending. Provider may be omitted (defaults to padin-inbox); `auto`, `xmtp` and unknown providers are rejected.

List input: `{"address":"<your verified recipient wallet>","limit":20,"cursor":"","provider":"padin-inbox"}`. Newest first by timestamp and ID. Limit 1–50, default 20. Pass the returned `next_cursor` unchanged for the next page; empty means no next page. The cursor is bound to the requested address but is not an access credential. Lists contain ID, from, to, created_at and status, not body previews.

Detail input: `{"address":"<your verified recipient wallet>","id":"<message ID from inbox>","provider":"padin-inbox"}`. Only the recipient can fetch details. A sender receives its own send result and can retry its own send, but does not gain arbitrary access to recipient inboxes. Nonexistent IDs and another recipient's IDs both return 404 message_not_found.

Responses use the existing QueryResult envelope (id/provider/status/latency_ms/fee_usd/data). A successful envelope has status=succeeded; message data has status=stored, meaning committed to this provider inbox, **not delivered to a wallet app or read**. List/get are read-only and do not mark messages read.

## Authorization

- Existing `data:read` API keys and OAuth grants cannot read or send private messages. No automatic permission upgrade; private-message API keys are not implemented.
- Browser REST uses the current authenticated **app** session, approved Origin and a currently linked, signature-verified wallet. Account access is the authorization boundary: a user signing in through linked X can use an already verified wallet. This is not a new signature for every read/send. X-only users must link and verify a wallet first.
- MCP OAuth request scopes must include `data:read` and explicitly include `messages:read`, `messages:send` or both. Empty scope defaults to data:read only. The consent page lists the actual requested permissions, requires selection of an owned verified wallet, and never auto-approves.
- Grants store that selected wallet. Every tool invocation checks scope, grant-wallet match and current ownership; adding another wallet later does not expand an existing grant. Connected-app listings show scope and bound wallet.
- Access tokens last up to 15 minutes, grants 30 days, refresh tokens rotate. Refresh preserves scope and wallet; explicit refresh scope must equal the original normalized grant scope. Scope escalation and narrowing by refresh are rejected; create a new grant for different permissions. Revocation stops subsequent calls and refresh.
- OAuth bearer tokens remain MCP-only, not REST tokens. The admin audience does not gain inbox access or message-reading administration endpoints.

Client-specific scope-request commands still need verification against installed client versions. Merely restarting a client with a data-only grant is insufficient: initiate a new OAuth flow with the explicit message scopes. Do not ask users to paste tokens or private keys into AI chat.

## Reliability, limits and metering

The service has a 10-second request deadline and checks provider enabled state. No cross-provider fallback for writes. A timeout/503 is an unknown send outcome: retry the **same key and identical payload**. Browser state preserves that payload while the page remains mounted; it is not persisted across reloads or navigation. Keep the key and payload when recovering outside the page.

The adapter serializes idempotency and send quotas per workspace in a database transaction. Message insert and one successful send-operation record commit together. Same workspace/key/same content returns the original ID; changing sender, recipient or body returns 409 idempotency_conflict. Retries do not consume the send quota or double-count successful sends. Read calls get normal operation records containing only capability/status/timing, never body/address/recipient. Input/permission failures are not general message-history entries.

Limits: existing gateway 60 requests/minute/workspace; message service 30 calls/minute/workspace across REST/MCP; new sends 10/minute and 100/rolling day/workspace. Idempotent retries remain subject to the general request limits. Fees are zero in preview. A 429 must not trigger another provider or a new key.

Errors: 400 invalid_request, 401 authentication required/invalid token, 403 message_permission_required, 404 message_not_found, 409 idempotency_conflict, 429 rate_limited, 503 message_service_unavailable. MCP marks failures as tool errors with sanitized messages.

## Implementation and rollout

`messaging` owns contracts/use cases, `providers/padininbox` owns message storage, `identity` owns wallet verification and ownership lookup, `mcpoauth` owns grants, and `operations` owns general metering records. PADIN is registered alongside other providers for admin capability mapping and enabled-state control, but message writes do not enter automatic read failover.

Migration 005 adds the inbox table, indexes, provider route, OAuth request scope and grant wallet binding. It expands the grant-scope constraint without changing existing grant scopes. Back up before deployment and deploy the API and consent UI together. Old code ignores the new provider and rejects extended-scope tokens; rolling back does not delete messages or revoke tokens automatically. Do not drop inbox tables or restore over production to roll back code. Stop pending authorization flows and reauthorize as needed after a rollback.

Verification: Go race tests use only a dedicated `_test` database and synthetic wallets; they cover signed login, old-token denial, X-only denial, sender spoofing, recipient isolation, unregistered recipients, concurrent idempotency, operation deduplication, cursor binding, quotas, wallet-scoped OAuth issue/refresh/revoke and REST/MCP schema parity. No real recipient messages or production writes were used. Browser click/visual validation remains outstanding when no browser is connected.
