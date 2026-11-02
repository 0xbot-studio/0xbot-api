package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/tokenbound"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Read-only illustrative bindings, never wallet creation or funding.
func tokenBoundSmoke(ctx context.Context, session *mcp.ClientSession, call func(string, string, any, string, any) error, secret string) error {
	for _, network := range data.Networks() {
		in := tokenbound.ResolveRequest{Network: network.ID, TokenContract: "0xcfcfcfcfcfcfcfcfcfcfcfcfcfcfcfcfcfcfcfcf", TokenID: "123", Implementation: "0xbebebebebebebebebebebebebebebebebebebebe", Salt: "0x" + strings.Repeat("0", 64)}
		var result tokenbound.Result
		if err := call("POST", "/v1/data/token-bound-account/resolve", in, secret, &result); err != nil {
			return fmt.Errorf("ERC-6551 REST resolve %s: %w", network.ID, err)
		}
		if result.Status != "succeeded" || result.Data.Network != network.ID || result.Data.TokenID != in.TokenID || result.Data.Registry != tokenbound.Registry || !data.ValidAddress(result.Data.Address) || result.Data.BlockNumber == "" {
			return fmt.Errorf("ERC-6551 REST snapshot mismatch")
		}
		get := tokenbound.GetRequest{Network: network.ID, Address: result.Data.Address}
		var inspected tokenbound.Result
		err := call("POST", "/v1/data/token-bound-account/get", get, secret, &inspected)
		if result.Data.Deployed {
			if err != nil || inspected.Data.Address != get.Address || inspected.Data.Verification != "canonical_proxy" {
				return fmt.Errorf("ERC-6551 deployed inspection mismatch: %v", err)
			}
		} else if !errors.Is(err, httpStatusError(http.StatusUnprocessableEntity)) {
			return fmt.Errorf("ERC-6551 undeployed lookup must return 422: %v", err)
		}
		tool, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "resolve_token_bound_account", Arguments: in})
		if err != nil || tool.IsError {
			return fmt.Errorf("ERC-6551 MCP resolve %s: %v", network.ID, err)
		}
		raw, err := json.Marshal(tool.StructuredContent)
		if err != nil {
			return err
		}
		var mcpResult tokenbound.Result
		if json.Unmarshal(raw, &mcpResult) != nil || mcpResult.Data.Address != result.Data.Address || mcpResult.Data.Network != network.ID || mcpResult.Status != "succeeded" {
			return fmt.Errorf("ERC-6551 MCP/REST result mismatch")
		}
		tool, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "get_token_bound_account", Arguments: get})
		if err != nil {
			return err
		}
		if tool.IsError == mcpResult.Data.Deployed {
			return fmt.Errorf("ERC-6551 MCP inspection status mismatch")
		}
		fmt.Printf("ERC-6551 %s REST/MCP resolve and inspect verified (deployed=%t).\n", network.ID, result.Data.Deployed)
	}
	return nil
}
