// Command smoke exercises a deployed preview using a newly generated, unfunded EOA.
// It never prints credentials, sends funds, or signs transaction authorizations.
package main

import (
	"0xbot.org/api/internal/data"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strings"
	"time"
)

type httpStatusError int

func (s httpStatusError) Error() string { return fmt.Sprintf("HTTP %d", int(s)) }

func main() {
	api := flag.String("api", "http://localhost:8095", "API origin")
	app := flag.String("app", "http://localhost:3191", "App origin")
	messagesOnly := flag.Bool("messages-only", false, "Verify messages between two newly generated test wallets only")
	roomsOnly := flag.Bool("rooms-only", false, "Verify one explicitly labeled test room using new unfunded wallets")
	swapsOnly := flag.Bool("swaps-only", false, "Verify live swap quotes without signing or sending transactions")
	flag.Parse()
	check := run
	if *messagesOnly {
		check = messageSmoke
	}
	if *roomsOnly {
		check = roomSmoke
	}
	if *swapsOnly {
		check = swapSmoke
	}
	if e := check(*api, *app); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run(api, app string) error {
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 20 * time.Second}
	call := func(method, path string, body any, bearer string, out any) error {
		// Pace deployment checks below the shared 60-request workspace budget.
		time.Sleep(1100 * time.Millisecond)
		var raw []byte
		if body != nil {
			raw, _ = json.Marshal(body)
		}
		req, e := http.NewRequest(method, api+path, bytes.NewReader(raw))
		if e != nil {
			return e
		}
		req.Header.Set("Origin", app)
		req.Header.Set("Content-Type", "application/json")
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		res, e := client.Do(req)
		if e != nil {
			return fmt.Errorf("%s %s: transport failure", method, path)
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			return fmt.Errorf("%s %s: %w", method, path, httpStatusError(res.StatusCode))
		}
		if out != nil {
			return json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(out)
		}
		return nil
	}
	key, e := crypto.GenerateKey()
	if e != nil {
		return e
	}
	address := crypto.PubkeyToAddress(key.PublicKey).Hex()
	var challenge struct{ ID, Message string }
	if e = call("POST", "/v1/auth/wallet/challenge", map[string]string{"address": address}, "", &challenge); e != nil {
		return e
	}
	hash := crypto.Keccak256([]byte(fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(challenge.Message), challenge.Message)))
	sig, e := crypto.Sign(hash, key)
	if e != nil {
		return e
	}
	sig[64] += 27
	if e = call("POST", "/v1/auth/wallet/verify", map[string]string{"id": challenge.ID, "signature": "0x" + hex.EncodeToString(sig)}, "", nil); e != nil {
		return e
	}
	defer call("POST", "/v1/auth/logout", nil, "", nil)
	var created struct {
		Secret string
		Key    struct{ ID string }
	}
	if e = call("POST", "/v1/keys", map[string]string{"name": "Deployment smoke test — revoke after use"}, "", &created); e != nil {
		return e
	}
	defer call("DELETE", "/v1/keys/"+created.Key.ID, nil, "", nil)
	for _, route := range []struct{ network, provider string }{
		{"eip155:1", "publicnode"}, {"eip155:1", "drpc"}, {"eip155:1", "auto"},
		{"eip155:56", "publicnode-bsc"}, {"eip155:56", "bnbchain-bsc"}, {"eip155:56", "auto"},
		{"eip155:137", "publicnode-polygon"}, {"eip155:137", "drpc-polygon"}, {"eip155:137", "auto"},
	} {
		var result data.Result
		if e = call("POST", "/v1/data/balance", map[string]string{"address": "0x0000000000000000000000000000000000000000", "network": route.network, "provider": route.provider}, created.Secret, &result); e != nil {
			return e
		}
		network, _ := data.FindNetwork(route.network)
		if result.Status != "succeeded" || result.Data.Network != route.network || result.Data.Symbol != network.Symbol || result.Data.Asset != network.Asset || result.Data.Decimals != 18 {
			return fmt.Errorf("route %s returned incorrect asset or status", route.provider)
		}
		if route.provider != "auto" && result.Provider != route.provider {
			return fmt.Errorf("explicit route changed")
		}
		fmt.Printf("%s / %s → %s: %s succeeded\n", route.network, route.provider, result.Provider, result.Data.Symbol)
	}
	for _, token := range data.Tokens() {
		var result data.Result
		if e = call("POST", "/v1/data/token-balance", data.TokenRequest{Address: address, Network: token.Network, Contract: token.Contract, Provider: "auto"}, created.Secret, &result); e != nil {
			return fmt.Errorf("%s %s: %w", token.Network, token.Symbol, e)
		}
		if result.Status != "succeeded" || result.Data.Contract != token.Contract || result.Data.Symbol != token.Symbol || result.Data.Decimals != token.Decimals || result.Data.Variant != token.Variant || result.Data.Amount != "0" {
			return fmt.Errorf("incorrect token metadata or new-wallet balance: %s %s", token.Network, token.Symbol)
		}
		fmt.Printf("REST %s %s (%s): succeeded\n", token.Network, token.Symbol, token.Variant)
	}
	for _, route := range []struct{ network, provider string }{
		{"eip155:1", "publicnode"}, {"eip155:1", "drpc"},
		{"eip155:56", "publicnode-bsc"}, {"eip155:56", "bnbchain-bsc"},
		{"eip155:137", "publicnode-polygon"}, {"eip155:137", "drpc-polygon"},
	} {
		var result data.BalancesResult
		if e = call("POST", "/v1/data/balances", data.Request{Address: address, Network: route.network, Provider: route.provider}, created.Secret, &result); e != nil {
			return fmt.Errorf("batch %s: %w", route.provider, e)
		}
		if result.Status != "succeeded" || result.Provider != route.provider || len(result.Data) != 3 {
			return fmt.Errorf("incorrect batch shape: %s", route.provider)
		}
		seen := map[string]bool{}
		for _, balance := range result.Data {
			if balance.Network != route.network || balance.BlockNumber != result.Data[0].BlockNumber || balance.Amount != "0" || seen[balance.Asset] {
				return fmt.Errorf("incorrect batch consistency: %s", route.provider)
			}
			seen[balance.Asset] = true
		}
		fmt.Printf("REST %s: three-asset same-block batch succeeded\n", route.provider)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "0xbot-deployment-smoke", Version: "1"}, nil)
	session, e := mcpClient.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: api + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport{secret: created.Secret}, Timeout: 20 * time.Second}}, nil)
	if e != nil {
		return fmt.Errorf("MCP initialization failed: %w", e)
	}
	defer session.Close()
	if !strings.Contains(session.InitializeResult().Instructions, "BSC/BNB Smart Chain=eip155:56") {
		return fmt.Errorf("MCP server missing BNB routing instructions")
	}
	tools, e := session.ListTools(ctx, nil)
	if e != nil {
		return fmt.Errorf("MCP tool discovery failed: %w", e)
	}
	nativeFound := false
	for _, tool := range tools.Tools {
		if tool.Name == "get_native_balance" {
			nativeFound = strings.Contains(tool.Description, "BNB Smart Chain") && tool.Annotations != nil && tool.Annotations.ReadOnlyHint
		}
	}
	if len(tools.Tools) != 27 || !nativeFound {
		return fmt.Errorf("MCP discovery metadata mismatch")
	}
	fmt.Println("MCP server instructions, twenty-seven-tool discovery and native BNB metadata verified.")
	if e = priceSmoke(ctx, session, call, created.Secret); e != nil {
		return e
	}
	catalog, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_supported_tokens", Arguments: map[string]string{}})
	if e != nil || catalog.IsError || len(catalog.Content) == 0 {
		return fmt.Errorf("MCP token catalog failed: %v", e)
	}
	for _, network := range data.Networks() {
		toolResult, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_native_balance", Arguments: map[string]string{"address": address, "network": network.ID, "provider": "auto"}})
		if e != nil {
			return fmt.Errorf("MCP tool call failed: %w", e)
		}
		if toolResult.IsError || len(toolResult.Content) == 0 {
			return fmt.Errorf("MCP tool returned an error or empty content")
		}
		fmt.Printf("MCP %s real balance tool call: succeeded\n", network.ID)
		for _, tool := range []string{"get_token_balance", "get_common_balances"} {
			args := map[string]string{"address": address, "network": network.ID, "provider": "auto"}
			if tool == "get_token_balance" {
				for _, token := range data.Tokens() {
					if token.Network == network.ID {
						args["contract"] = token.Contract
						break
					}
				}
			}
			result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
			if err != nil || result.IsError || len(result.Content) == 0 {
				return fmt.Errorf("MCP %s %s failed: %v", network.ID, tool, err)
			}
			fmt.Printf("MCP %s %s: succeeded\n", network.ID, tool)
		}
	}
	if e = tokenBoundSmoke(ctx, session, call, created.Secret); e != nil {
		return e
	}
	if e = nftSmoke(ctx, session, call, created.Secret); e != nil {
		return e
	}
	if e = call("DELETE", "/v1/keys/"+created.Key.ID, nil, "", nil); e != nil {
		return e
	}
	if e = call("POST", "/v1/data/balance", map[string]string{"address": address, "network": "eip155:1"}, created.Secret, nil); !errors.Is(e, httpStatusError(http.StatusUnauthorized)) {
		return fmt.Errorf("revoked key did not return HTTP 401: %v", e)
	}
	fmt.Println("Wallet signature, account creation, API key, real provider queries and revocation verified.")
	if e = oauthSmoke(client, api, app, address); e != nil {
		return e
	}
	return nil
}

type bearerTransport struct{ secret string }

func (t bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	timer := time.NewTimer(1100 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-req.Context().Done():
		return nil, req.Context().Err()
	case <-timer.C:
	}
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+t.secret)
	return http.DefaultTransport.RoundTrip(clone)
}
