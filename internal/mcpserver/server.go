package mcpserver

import (
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/messaging"
	"0xbot.org/api/internal/operations"
	"0xbot.org/api/internal/rooms"
	"context"
	"fmt"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
)

func addTool[In, Out any](server *mcp.Server, tools *[]*mcp.Tool, tool *mcp.Tool, handler mcp.ToolHandlerFor[In, Out]) {
	// Static Go types are the source for both MCP discovery and the admin view.
	in, err := jsonschema.For[In](nil)
	if err != nil {
		panic(err)
	}
	out, err := jsonschema.For[Out](nil)
	if err != nil {
		panic(err)
	}
	tool.InputSchema, tool.OutputSchema = in, out
	if tool.Name == "get_common_balances" {
		// A successful batch is never nil and always contains exactly three assets.
		// Reflection alone permits null slices, which would contradict REST.
		payload := out.Properties["data"]
		payload.Type, payload.Types = "array", nil
		count := 3
		payload.MinItems, payload.MaxItems = &count, &count
	}
	mcp.AddTool(server, tool, handler)
	*tools = append(*tools, tool)
}

func New(service *operations.Service, workspace func(context.Context) string, messages ...*messaging.Service) http.Handler {
	server, _ := buildServer(service, workspace, messages...)
	return mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true})
}

// NewWithRooms keeps the discussion dependency explicit without breaking existing integrations.
func NewWithRooms(service *operations.Service, workspace func(context.Context) string, messages *messaging.Service, discussion *rooms.Service) http.Handler {
	server, _ := buildServerWithRooms(service, workspace, messages, discussion)
	return mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true})
}

// Definitions are taken from the same registration path as tools/list.
// This builds metadata only; it does not connect a client or execute a tool.
func Definitions() (string, []*mcp.Tool) {
	_, tools := buildServer(nil, func(context.Context) string { return "" })
	return instructions, tools
}

func buildServer(service *operations.Service, workspace func(context.Context) string, messages ...*messaging.Service) (*mcp.Server, []*mcp.Tool) {
	var msg *messaging.Service
	if len(messages) > 0 {
		msg = messages[0]
	}
	return buildServerWithRooms(service, workspace, msg, nil)
}
func buildServerWithRooms(service *operations.Service, workspace func(context.Context) string, msg *messaging.Service, discussion *rooms.Service) (*mcp.Server, []*mcp.Tool) {
	var tools []*mcp.Tool
	server := mcp.NewServer(&mcp.Implementation{Name: "0xbot", Version: "0.10.0"}, &mcp.ServerOptions{Instructions: instructions})
	addRoomTools(server, &tools, discussion)
	addMessageTools(server, &tools, msg)
	addSwapTools(server, &tools, service, workspace)
	addPriceTools(server, &tools, service, workspace)
	addNFTTools(server, &tools, service, workspace)
	addTokenBoundTools(server, &tools, service, workspace)
	addTool(server, &tools, &mcp.Tool{Name: "get_supported_tokens", Description: "List supported USDT/USDC contracts by network, exact address, decimals and issuance variant. BSC uses Binance-Peg; Polygon USDT is USDT0 and USDC is Circle-issued. Polygon USDC.e is not included."}, func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, struct {
		Tokens []data.Token `json:"tokens"`
	}, error) {
		return nil, struct {
			Tokens []data.Token `json:"tokens"`
		}{data.Tokens()}, nil
	})
	addTool(server, &tools, &mcp.Tool{Name: "get_token_balance", Description: "Read the balance of an exact curated token contract from get_supported_tokens. Specify network and contract. No approval, signature or transaction. Amount is an integer string in token base units, with verified decimals and asset variant."}, func(ctx context.Context, req *mcp.CallToolRequest, in data.TokenRequest) (*mcp.CallToolResult, data.Result, error) {
		w := workspace(ctx)
		if w == "" {
			return nil, data.Result{}, fmt.Errorf("authentication_required")
		}
		out, e := service.TokenBalance(ctx, w, in)
		if e != nil {
			return toolFailure(e), data.Result{}, nil
		}
		return nil, out, nil
	})
	addTool(server, &tools, &mcp.Tool{Name: "get_common_balances", Description: "Read native asset and two curated stablecoins on one selected network at the same block and provider. Ethereum: ETH/USDT/USDC. BSC: BNB and Binance-Peg USDT/USDC. Polygon: POL/USDT0/native USDC. All-or-error; no partial balances. 20 batch calls/minute/workspace. One operation, no funds move."}, func(ctx context.Context, req *mcp.CallToolRequest, in data.Request) (*mcp.CallToolResult, data.BalancesResult, error) {
		w := workspace(ctx)
		if w == "" {
			return nil, data.BalancesResult{}, fmt.Errorf("authentication_required")
		}
		out, e := service.Balances(ctx, w, in)
		if e != nil {
			return toolFailure(e), data.BalancesResult{}, nil
		}
		return nil, out, nil
	})
	addTool(server, &tools, &mcp.Tool{Name: "get_native_balance", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}, Description: "Query a wallet/address native coin balance / 查询地址的 ETH、BNB、POL 余额. Use for 'BNB balance on BSC / BNB Smart Chain': network=eip155:56. Ethereum ETH: eip155:1; Polygon PoS POL: eip155:137. Supply address and network; omit provider for automatic free routing. No token contract or prior discovery call is needed. Returns exact base-unit amount, decimals, block number, observation time and provider. Read-only; no signature, transaction or paid fallback. Not ERC-20, NFT, receipt or transaction lookup."}, func(ctx context.Context, req *mcp.CallToolRequest, in data.Request) (*mcp.CallToolResult, data.Result, error) {
		w := workspace(ctx)
		if w == "" {
			return nil, data.Result{}, fmt.Errorf("authentication_required")
		}
		out, e := service.Balance(ctx, w, in)
		if e != nil {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "Balance query failed. Check network, address and provider availability."}}}, data.Result{}, nil
		}
		return nil, out, nil
	})
	return server, tools
}

func toolFailure(err error) *mcp.CallToolResult {
	message := "Balance query failed. Check network, contract, provider and availability."
	if err == data.ErrRateLimited {
		message = "rate_limited: common balances allows 20 calls per minute per workspace. Retry later."
	}
	if err == data.ErrInput {
		message = "invalid_request: use a supported network and exact token contract from get_supported_tokens."
	}
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: message}}}
}
