package main

import (
	"0xbot.org/api/internal/apikeys"
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/swaps"
	"context"
	"encoding/json"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"strings"
	"time"
)

func swapSmoke(api, app string) error {
	a, e := newMessageActor(api, app)
	if e != nil {
		return e
	}
	defer a.call("POST", "/v1/auth/logout", nil, "", 200, nil)
	var k apikeys.Created
	if e = a.call("POST", "/v1/keys", map[string]string{"name": "Swap deployment smoke — temporary"}, "", 200, &k); e != nil {
		return e
	}
	defer a.call("DELETE", "/v1/keys/"+k.Key.ID, nil, "", 200, nil)
	for _, network := range []string{"eip155:1", "eip155:8453"} {
		var r swaps.TokensResult
		if e = a.call("POST", "/v1/swaps/tokens", swaps.TokensInput{Network: network}, k.Secret, 200, &r); e != nil {
			return e
		}
		if r.Provider != "lifi" || len(r.Data) == 0 {
			return fmt.Errorf("empty swap catalog")
		}
		fmt.Printf("Live swap catalog %s: %d tokens\n", network, len(r.Data))
	}
	in := swaps.QuoteInput{FromNetwork: "eip155:1", ToNetwork: "eip155:8453", FromToken: swaps.Native, ToToken: swaps.Native, Amount: "10000000000000000", Wallet: a.address, SlippageBPS: 50}
	validate := func(q swaps.QuoteResult) error {
		if q.Provider != "lifi" || q.Status != "succeeded" || q.Data.Request != in || q.Data.Transaction.From != a.address || q.Data.Transaction.Value != in.Amount || !swaps.Amount(q.Data.MinimumAmount, true) || time.Now().After(q.Data.ExpiresAt) {
			return fmt.Errorf("live swap quote mismatch")
		}
		return nil
	}
	var quote swaps.QuoteResult
	if e = a.call("POST", "/v1/swaps/quote", in, k.Secret, 200, &quote); e != nil {
		return e
	}
	if e = validate(quote); e != nil {
		return e
	}
	fmt.Println("REST: live Ethereum → Base quote and transaction preparation verified; no broadcast")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	c := mcp.NewClient(&mcp.Implementation{Name: "0xbot-swap-smoke", Version: "1"}, nil)
	session, e := c.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: api + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport{secret: k.Secret}, Timeout: 25 * time.Second}}, nil)
	if e != nil {
		return e
	}
	defer session.Close()
	tools, e := session.ListTools(ctx, nil)
	if e != nil {
		return e
	}
	count := 0
	for _, tool := range tools.Tools {
		if strings.HasPrefix(tool.Name, "get_swap_") {
			if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
				return fmt.Errorf("swap tool not read-only")
			}
			count++
		}
	}
	if count != 3 {
		return fmt.Errorf("swap discovery incomplete")
	}
	result, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_swap_quote", Arguments: in})
	if e != nil {
		return e
	}
	if result.IsError {
		return fmt.Errorf("MCP live swap quote unavailable")
	}
	raw, e := json.Marshal(result.StructuredContent)
	if e != nil {
		return e
	}
	if e = json.Unmarshal(raw, &quote); e != nil {
		return e
	}
	if e = validate(quote); e != nil {
		return e
	}
	fmt.Println("MCP: three read-only swap tools and live quote verified")
	successfulRoutes := 0
	for _, route := range []struct{ network, provider string }{{"eip155:1", "publicnode"}, {"eip155:1", "drpc"}, {"eip155:56", "publicnode-bsc"}, {"eip155:56", "bnbchain-bsc"}} {
		var r data.Result
		if e = a.call("POST", "/v1/data/balance", data.Request{Address: a.address, Network: route.network, Provider: route.provider}, k.Secret, 200, &r); e != nil {
			fmt.Printf("Upstream route %s currently unavailable\n", route.provider)
			continue
		}
		if r.Provider != route.provider || r.Status != "succeeded" {
			return fmt.Errorf("balance route mismatch")
		}
		successfulRoutes++
		fmt.Printf("Existing balance route %s verified\n", route.provider)
		if successfulRoutes == 2 {
			break
		}
	}
	if successfulRoutes < 2 {
		return fmt.Errorf("fewer than two existing provider routes available")
	}
	var automatic data.Result
	if e = a.call("POST", "/v1/data/balance", data.Request{Address: a.address, Network: "eip155:1", Provider: "auto"}, k.Secret, 200, &automatic); e != nil {
		return e
	}
	fmt.Printf("Automatic Ethereum route verified via %s\n", automatic.Provider)
	if e = a.call("DELETE", "/v1/keys/"+k.Key.ID, nil, "", 200, nil); e != nil {
		return e
	}
	if e = a.call("POST", "/v1/swaps/quote", in, k.Secret, 401, nil); e != nil {
		return e
	}
	fmt.Println("Temporary key revoked; no token approval or funds transfer performed")
	return nil
}
