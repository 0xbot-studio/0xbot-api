package mcpserver

import (
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/operations"
	"0xbot.org/api/internal/swaps"
	"context"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func addSwapTools(server *mcp.Server, tools *[]*mcp.Tool, s *operations.Service, workspace func(context.Context) string) {
	addTool(server, tools, &mcp.Tool{Name: "get_swap_tokens", Description: "List LI.FI EVM token contracts and decimals. Exact addresses only; zero address is native. Not the balance token catalog.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, _ *mcp.CallToolRequest, in swaps.TokensInput) (*mcp.CallToolResult, swaps.TokensResult, error) {
		w := workspace(ctx)
		if w == "" {
			return nil, swaps.TokensResult{}, fmt.Errorf("authentication_required")
		}
		out, e := s.SwapTokens(ctx, w, in)
		if e != nil {
			code := "provider_unavailable"
			if e == data.ErrInput || e == data.ErrRateLimited || e == data.ErrProvider {
				code = e.Error()
			}
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: code}}}, swaps.TokensResult{}, nil
		}
		return nil, out, nil
	})
	addTool(server, tools, &mcp.Tool{Name: "get_swap_quote", Description: "Prepare a LI.FI EVM swap or bridge quote. Read-only: never signs or submits. Use exact contracts and base units, show minimum output, costs and expiry. Wallet must explicitly authorize any approval and transaction. Quote expires after 60 seconds.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, _ *mcp.CallToolRequest, in swaps.QuoteInput) (*mcp.CallToolResult, swaps.QuoteResult, error) {
		w := workspace(ctx)
		if w == "" {
			return nil, swaps.QuoteResult{}, fmt.Errorf("authentication_required")
		}
		out, e := s.SwapQuote(ctx, w, in)
		if e != nil {
			code := "provider_unavailable"
			if e == data.ErrInput || e == data.ErrRateLimited || e == data.ErrProvider {
				code = e.Error()
			}
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: code}}}, swaps.QuoteResult{}, nil
		}
		return nil, out, nil
	})
	addTool(server, tools, &mcp.Tool{Name: "get_swap_status", Description: "Read LI.FI transaction status. Only completed means destination completion. Unknown or unavailable does not mean failed: never resubmit automatically.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, _ *mcp.CallToolRequest, in swaps.StatusInput) (*mcp.CallToolResult, swaps.StatusResult, error) {
		w := workspace(ctx)
		if w == "" {
			return nil, swaps.StatusResult{}, fmt.Errorf("authentication_required")
		}
		out, e := s.SwapStatus(ctx, w, in)
		if e != nil {
			code := "provider_unavailable"
			if e == data.ErrInput || e == data.ErrRateLimited || e == data.ErrProvider {
				code = e.Error()
			}
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: code}}}, swaps.StatusResult{}, nil
		}
		return nil, out, nil
	})
}
