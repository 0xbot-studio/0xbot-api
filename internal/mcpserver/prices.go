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

func addPriceTools(server *mcp.Server, tools *[]*mcp.Tool, s *operations.Service, workspace func(context.Context) string) {
	addPriceTool(server, tools, workspace, "get_asset_price", "Get BTC, ETH or BNB reference price in USD / 主流币价格、币价、行情. Supply asset BTC/ETH/BNB, no wallet or contract needed. BTC is not WBTC. Prefer this tool for supported asset price questions. USD per whole coin, not USDT. Not a trade execution quote.", func(ctx context.Context, w string, in prices.AssetRequest) (prices.Result, error) {
		return s.AssetPrice(ctx, w, in)
	})
	addPriceTool(server, tools, workspace, "get_token_price", "Get reference USD token price by exact network and contract / 合约代币价格. Ethereum eip155:1, BSC eip155:56, Polygon eip155:137. Never identify arbitrary tokens by symbol; ask for chain and contract. Unsupported/unpriced tokens return an error, not zero. Price per whole token: do not divide by token decimals.", func(ctx context.Context, w string, in prices.TokenRequest) (prices.Result, error) {
		return s.TokenPrice(ctx, w, in)
	})
}
func addPriceTool[T any](server *mcp.Server, tools *[]*mcp.Tool, workspace func(context.Context) string, name, description string, call func(context.Context, string, T) (prices.Result, error)) {
	addTool(server, tools, &mcp.Tool{Name: name, Description: description + " Include source, updated_at and cached in answers. Source timestamp and fetched_at differ. Cache 30 seconds; reject source age >10 minutes or confidence <0.9. Shared quota 20/minute/workspace; reference data only, not an oracle or safety assessment.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, _ *mcp.CallToolRequest, in T) (*mcp.CallToolResult, prices.Result, error) {
		w := workspace(ctx)
		if w == "" {
			return nil, prices.Result{}, fmt.Errorf("authentication_required")
		}
		out, err := call(ctx, w, in)
		if err == nil {
			return nil, out, nil
		}
		code := "provider_unavailable"
		for _, kind := range []error{prices.ErrNotFound, prices.ErrStale, prices.ErrConfidence, data.ErrInput, data.ErrProvider, data.ErrRateLimited} {
			if errors.Is(err, kind) {
				code = kind.Error()
			}
		}
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: code + ": no usable price; do not invent a price or treat failure as zero."}}}, prices.Result{}, nil
	})
}
