# ERC-6551 read-only account capabilities

API 0.5.0 adds two optional capabilities. Existing users do not need an NFT or a new wallet. Both tools use `data:read`; REST uses existing API keys/sessions, MCP also accepts the existing MCP OAuth grant. This does not add smart-contract-wallet login or extend an OAuth grant to transactions.

| Capability | MCP | REST POST |
| --- | --- | --- |
| `data.token_bound_account.resolve` | `resolve_token_bound_account` | `/v1/data/token-bound-account/resolve` |
| `data.token_bound_account.get` | `get_token_bound_account` | `/v1/data/token-bound-account/get` |

## Resolve

Example inputs use the illustrative NFT/implementation from the ERC specification, not a recommended wallet implementation. Do not fund these addresses.

```json
{
  "network": "eip155:1",
  "token_contract": "0xcfcfcfcfcfcfcfcfcfcfcfcfcfcfcfcfcfcfcfcf",
  "token_id": "123",
  "implementation": "0xbebebebebebebebebebebebebebebebebebebebe",
  "salt": "0x0000000000000000000000000000000000000000000000000000000000000000"
}
```

All five fields are required. `token_id` is a canonical decimal uint256 string, never a JSON number. `salt` is exactly 32 bytes in 0x-prefixed hex. Zero implementation and NFT contract addresses are rejected. Omit `provider` for automatic free routing, or explicitly select a supported network route. The computed Ethereum address for this example is `0x3b81c21f0be3b5380b62d6ddb02246dff803df5a`; its deployment status is a separate live observation, not implied by this example.

Only the canonical registry `0x000000006551c19487814612e58fe06813775758` is supported. Account and NFT must be on the same supported network: Ethereum `eip155:1`, BSC `eip155:56`, Polygon PoS `eip155:137`. There is no assumed default implementation or salt and no claim of a unique account per NFT. Custom registries and cross-chain bindings are intentionally out of scope.

## Inspect and query balances

Call `get_token_bound_account` with `address` and `network` (optional `provider`). It accepts only deployed accounts whose runtime matches the canonical 173-byte proxy and whose address matches the canonical registry CREATE2 derivation. Binding metadata comes from the immutable proxy runtime; arbitrary implementation methods are not called. It does not enumerate all accounts belonging to an NFT.

Pass a returned account's `address` and `network` to `get_native_balance` or `get_common_balances`. For a single curated stablecoin use `get_supported_tokens`, then `get_token_balance`. Balance calls are separate operations and may observe a later block; they are not an atomic account-plus-balance snapshot.

## Output and limitations

Both tools return the existing `id/provider/status/latency_ms/fee_usd/data` envelope. `data` contains address, network, registry, token_contract, token_id, implementation, salt, deployed, implementation_deployed, verification, block_number and observed_at. Block and token IDs are decimal strings. All code observations in one result use one provider and one block number.

- `deployed=false`, `verification=counterfactual`: computed address has no runtime code at the observed block. Not proof that deployment is possible or that funds would be recoverable.
- `deployed=true`, `verification=canonical_proxy`: matching proxy structure/address. Not a compliance certificate or security audit.
- `implementation_deployed` reports only whether implementation code is nonempty. ERC-165/ERC-1271 behavior, NFT existence/ownership, signing authorization and registry availability are not verified.
- No creation, minting, signing, execution, custody, owner lookup, account indexer, custom registry or cross-chain control. Never request private keys, seed phrases or signing credentials.
- Resolve/get share a 20-call/minute/workspace limit across REST and MCP, separate from the batch-balance limit. One routed query produces one operation/usage record, including after fallback. Public preview fee is zero; no paid fallback.
- RPC failures can retry the complete read on another eligible provider in auto mode. Explicit provider selection never silently falls back. Timeouts return failure, not `deployed=false`.
- REST errors: 400 invalid request/provider, 401 missing or revoked auth, 422 `not_token_bound_account` for unsupported/absent proxy bindings, 429 rate limited (`Retry-After: 60`), 503 provider unavailable. MCP returns tool errors without partial success data. A non-account observation does not trip provider health or retry another provider.

## Verification

Offline tests cover the specification's runtime example and an independently verified registry address vector; uint256 boundaries; deployment/implementation distinctions; same-block reads; malformed/wrong-network responses; canonical-address matching; cancellation; routing; shared quotas; tenant history and key revocation; HTTP MCP execution and REST/MCP contract parity. Online `account(...)` was called read-only on the Ethereum canonical registry to independently verify the fixed vector. No account was created and no funds moved. Full production integration remains a deployment-stage check.

Reference: [ERC-6551 specification](https://eips.ethereum.org/EIPS/eip-6551).
