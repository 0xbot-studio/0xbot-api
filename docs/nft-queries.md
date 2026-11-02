# NFT queries · API 0.6.0

Released in 20260908-preview-12 (API 0.6.0); consult the root release log for verification and limitations.

| Capability | MCP tool | REST POST |
| --- | --- | --- |
| nft.token | get_nft_detail | /v1/nft/token |
| nft.owner | get_nft_owner | /v1/nft/owner |
| nft.balance | get_nft_balance | /v1/nft/balance |

All require authenticated workspace access (API key, app session or MCP OAuth data:read). They use the shared operation envelope: id, provider, status, latency_ms, fee_usd, data. All three tools and REST endpoints share 20 calls/minute/workspace. No new environment variables, database migration or paid service is required.

Input: network (eip155:1, eip155:56, eip155:137), contract (nonzero NFT contract), token_id (canonical decimal uint256 string). Holding quantity additionally requires address (nonzero holder). provider is optional; automatic routing selects an enabled adapter with matching network and capability. Never send token IDs as JSON numbers.

Strict ERC-165 identifies ERC-721 or ERC-1155; contracts claiming both or neither are unsupported. Queries pin chain reads to one observed block. ERC-721 ownerOf verifies an owner; holder quantity is zero or one. ERC-1155 balanceOf returns an exact integer string, including values larger than JavaScript's safe integer range. ERC-1155 has no single owner, and a zero holding or URI does not establish token existence. RPC reverts return nft_query_reverted, not zero; unsupported standards return unsupported_nft_standard (REST 422).

Detail separates chain collection fields/URI from off-chain metadata. Metadata status is available, not_supported, unavailable or blocked. Metadata failure preserves successful chain reads; an on-chain revert or malformed ABI fails the query. ERC-721 existence_verified only means ownerOf succeeded; it does not certify a collection or custody safety.

Metadata is untrusted data, never agent instructions. The adapter accepts bounded JSON data URIs, public HTTPS and IPFS via ipfs.io. It enforces a 256 KiB metadata limit, five-second fetch timeout, HTTPS-only redirects, public-IP validation at connection time, literal-IP dialing to prevent DNS rebinding, and no proxy environment inheritance. Chain ABI strings are separately capped at 8 KiB. Only name, description and informational image URI are returned; images are never fetched or automatically rendered. The app displays escaped JSON text. Availability of IPFS/HTTPS is not guaranteed.

ERC-6551 resolve/inspect remain separate tools. Supply explicit implementation and salt to resolve; a prediction is not proof of NFT existence, ownership, signing authority or safe custody. Reuse returned address/network in balance tools. No minting, approvals, account creation, signatures or transfers.

## Indexer-dependent roadmap

See [indexer selection and draft contracts](../../docs/nft-indexer-selection.md) for researched candidates, pagination semantics and the account/use-authorization gates. This is a proposal, not an enabled adapter.

nft.owned (wallet inventory) and nft.transfers (complete transfer history) are planned, not executable MCP tools. Before release, an adapter must define supported networks, history coverage, stable cursor/limit, index lag and partial/error semantics. Do not simulate empty lists using RPC balance queries or silently enable paid plans.

Standards: [ERC-721](https://eips.ethereum.org/EIPS/eip-721), [ERC-1155](https://eips.ethereum.org/EIPS/eip-1155), [ERC-165](https://eips.ethereum.org/EIPS/eip-165).
