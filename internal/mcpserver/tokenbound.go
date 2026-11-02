package mcpserver

import (
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/operations"
	"0xbot.org/api/internal/tokenbound"
	"context"
	"errors"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func addTokenBoundTools(server *mcp.Server, tools *[]*mcp.Tool, service *operations.Service, workspace func(context.Context) string) {
	addTool(server, tools, &mcp.Tool{Name: "resolve_token_bound_account", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}, Description: "Resolve an ERC-6551 NFT-bound account / 解析 NFT 绑定账户. Supply network, token_contract, decimal-string token_id, explicit implementation and bytes32 salt; omit provider for routing. Ethereum/BSC/Polygon same-chain only; canonical registry only. Computes an address and checks account/implementation code at one block. deployed=false is a counterfactual address, NOT a usable-wallet guarantee. Does not verify NFT existence, owner, implementation safety or signing authority. Never guess implementation or salt. No transactions. Shared 20 calls/minute/workspace with get_token_bound_account."}, func(ctx context.Context, _ *mcp.CallToolRequest, in tokenbound.ResolveRequest) (*mcp.CallToolResult, tokenbound.Result, error) {
		w := workspace(ctx)
		if w == "" {
			return nil, tokenbound.Result{}, fmt.Errorf("authentication_required")
		}
		out, err := service.ResolveTokenBoundAccount(ctx, w, in)
		if err != nil {
			return tokenBoundFailure(err), tokenbound.Result{}, nil
		}
		return nil, out, nil
	})
	addTool(server, tools, &mcp.Tool{Name: "get_token_bound_account", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}, Description: "Inspect a deployed ERC-6551 account / 查询 NFT 绑定账户 by address and network. Reads NFT binding, implementation and salt from canonical proxy bytecode and verifies its canonical-registry CREATE2 address. Same-chain Ethereum/BSC/Polygon only. Non-account or undeployed addresses return an error, not fabricated metadata. Does not enumerate all NFT accounts or certify ownership, authorization, ERC-165/ERC-1271 behavior or safety. For balances use returned address/network with get_native_balance or get_common_balances. Read-only; shared 20 calls/minute/workspace with resolve."}, func(ctx context.Context, _ *mcp.CallToolRequest, in tokenbound.GetRequest) (*mcp.CallToolResult, tokenbound.Result, error) {
		w := workspace(ctx)
		if w == "" {
			return nil, tokenbound.Result{}, fmt.Errorf("authentication_required")
		}
		out, err := service.GetTokenBoundAccount(ctx, w, in)
		if err != nil {
			return tokenBoundFailure(err), tokenbound.Result{}, nil
		}
		return nil, out, nil
	})
}
func tokenBoundFailure(err error) *mcp.CallToolResult {
	message := "provider_unavailable: token-bound account query could not be completed. Do not treat failure as undeployed or zero balance."
	switch {
	case errors.Is(err, data.ErrInput):
		message = "invalid_request: check supported network and address; resolve requires explicit nonzero implementation, token_contract, decimal uint256 token_id and bytes32 salt."
	case errors.Is(err, data.ErrProvider):
		message = "unsupported_provider: choose a route supporting the requested network and capability."
	case errors.Is(err, tokenbound.ErrNotAccount):
		message = "not_token_bound_account: no matching deployed canonical ERC-6551 proxy with a same-chain binding. Custom registries and cross-chain bindings are not supported."
	case errors.Is(err, data.ErrRateLimited):
		message = "rate_limited: resolve and get share 20 calls per minute per workspace. Retry later."
	}
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: message}}}
}
