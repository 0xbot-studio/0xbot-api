package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/prices"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func priceSmoke(ctx context.Context, session *mcp.ClientSession, call func(string, string, any, string, any) error, secret string) error {
	type query struct {
		tool, path, asset, network, contract string
		input                                any
	}
	var queries []query
	for _, asset := range []string{"BTC", "ETH", "BNB"} {
		queries = append(queries, query{tool: "get_asset_price", path: "/v1/prices/asset", asset: asset, input: prices.AssetRequest{Asset: asset}})
	}
	for _, token := range data.Tokens() {
		queries = append(queries, query{tool: "get_token_price", path: "/v1/prices/token", network: token.Network, contract: token.Contract, input: prices.TokenRequest{Network: token.Network, Contract: token.Contract}})
	}
	for _, q := range queries {
		validate := func(r prices.Result) error {
			n, ok := new(big.Rat).SetString(r.Data.Price)
			if r.Status != "succeeded" || r.Provider != "defillama-prices" || !ok || n.Sign() <= 0 || r.Data.Currency != "USD" || r.Data.Source == "" || r.Data.SourceAsset == "" || r.Data.Asset != q.asset || r.Data.Network != q.network || r.Data.Contract != q.contract || r.Data.FetchedAt.IsZero() || time.Since(r.Data.UpdatedAt) > 10*time.Minute {
				return fmt.Errorf("%s price result mismatch", q.tool)
			}
			return nil
		}
		var result prices.Result
		if err := call("POST", q.path, q.input, secret, &result); err != nil {
			return err
		}
		if err := validate(result); err != nil {
			return err
		}
		out, err := session.CallTool(ctx, &mcp.CallToolParams{Name: q.tool, Arguments: q.input})
		if err != nil {
			return err
		}
		if out.IsError {
			return fmt.Errorf("%s returned tool error", q.tool)
		}
		raw, err := json.Marshal(out.StructuredContent)
		if err != nil {
			return err
		}
		var structured prices.Result
		if err := json.Unmarshal(raw, &structured); err != nil {
			return err
		}
		if err := validate(structured); err != nil {
			return err
		}
		fmt.Printf("Price REST/MCP %s %s %s verified.\n", q.asset, q.network, q.contract)
	}
	return nil
}
