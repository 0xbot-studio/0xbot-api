package mcpserver

import (
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/operations"
	"0xbot.org/api/internal/prices"
	"context"
	"errors"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func addRWATools(server *mcp.Server, tools *[]*mcp.Tool, service *operations.Service, workspace func(context.Context) string) {
	addTool(server, tools, &mcp.Tool{Name: "get_rwa_token_balance", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}, Description: "Tokenized-equity balance / 代币化股票余额. Reads the balance of an exact curated tokenized-equity contract (asset_class=tokenized_equity from get_supported_tokens; Ethereum first release). Amount is an integer string in base units with verified decimals. Balance is an on-chain observation, NOT holding eligibility, a compliance verdict or a tradable quote; issuer availability restrictions still apply. No approval, signature or transaction. 20 calls/minute/workspace."}, func(ctx context.Context, _ *mcp.CallToolRequest, in data.EquityTokenRequest) (*mcp.CallToolResult, data.Result, error) {
		w := workspace(ctx)
		if w == "" {
			return nil, data.Result{}, fmt.Errorf("authentication_required")
		}
		out, err := service.RWATokenBalance(ctx, w, in)
		if err != nil {
			return rwaFailure(err), data.Result{}, nil
		}
		return nil, out, nil
	})
	addTool(server, tools, &mcp.Tool{Name: "get_equity_price", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}, Description: "Equity reference price / 股票参考价. Reference USD price for the underlying stock of a curated tokenized-equity contract (asset_class=tokenized_equity from get_supported_tokens). Exact decimal string with upstream confidence. Stale or market-closed observations return an error, never a last-known price. NOT an executable quote, NAV or valuation. 20 calls/minute/workspace shared with other price tools."}, func(ctx context.Context, _ *mcp.CallToolRequest, in prices.EquityRequest) (*mcp.CallToolResult, prices.Result, error) {
		w := workspace(ctx)
		if w == "" {
			return nil, prices.Result{}, fmt.Errorf("authentication_required")
		}
		out, err := service.EquityPrice(ctx, w, in)
		if err != nil {
			return rwaPriceFailure(err), prices.Result{}, nil
		}
		return nil, out, nil
	})
}
func rwaFailure(err error) *mcp.CallToolResult {
	message := "provider_unavailable: tokenized-equity balance query could not be completed. Do not treat failure as zero balance."
	switch {
	case errors.Is(err, data.ErrInput):
		message = "invalid_request: use an exact curated tokenized-equity contract with asset_class=tokenized_equity from get_supported_tokens; Ethereum eip155:1 only in this release."
	case errors.Is(err, data.ErrProvider):
		message = "unsupported_provider: choose a route supporting Ethereum tokenized-equity reads."
	case errors.Is(err, data.ErrRateLimited):
		message = "rate_limited: 20 calls per minute per workspace. Retry later."
	}
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: message}}}
}
func rwaPriceFailure(err error) *mcp.CallToolResult {
	message := "provider_unavailable: equity reference price could not be completed. Do not guess or substitute a price."
	switch {
	case errors.Is(err, data.ErrInput):
		message = "invalid_request: use an exact curated tokenized-equity contract with asset_class=tokenized_equity from get_supported_tokens; Ethereum eip155:1 only in this release."
	case errors.Is(err, prices.ErrStale):
		message = "price_stale: the underlying equity observation is older than 10 minutes (market may be closed). No last-known price is returned."
	case errors.Is(err, prices.ErrNotFound):
		message = "price_not_found: no equity feed covers this contract."
	case errors.Is(err, prices.ErrConfidence):
		message = "price_low_confidence: upstream confidence below the reporting threshold."
	case errors.Is(err, data.ErrRateLimited):
		message = "rate_limited: 20 price calls per minute per workspace. Retry later."
	}
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: message}}}
}
