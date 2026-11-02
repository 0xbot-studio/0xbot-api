package main

import (
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/nft"
	"context"
	"encoding/json"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Fixed public ERC-721 identity; verify runtime results, never mint or transfer.
func nftSmoke(ctx context.Context, session *mcp.ClientSession, call func(string, string, any, string, any) error, secret string) error {
	in := nft.TokenRequest{Network: "eip155:1", Contract: "0xbc4ca0eda7647a8ab7c2061c2e118a18a936f13d", TokenID: "1"}
	var owner nft.OwnerResult
	if err := call("POST", "/v1/nft/owner", in, secret, &owner); err != nil {
		return err
	}
	if owner.Status != "succeeded" || owner.Data.Standard != "erc721" || !data.ValidAddress(owner.Data.Owner) || owner.Data.BlockNumber == "" {
		return fmt.Errorf("NFT owner snapshot invalid")
	}
	var detail nft.DetailResult
	if err := call("POST", "/v1/nft/token", in, secret, &detail); err != nil {
		return err
	}
	if detail.Status != "succeeded" || detail.Data.Contract != in.Contract || !detail.Data.ExistenceVerified || detail.Data.Metadata.Status == "" {
		return fmt.Errorf("NFT detail invalid")
	}
	balanceIn := nft.BalanceRequest{Network: in.Network, Contract: in.Contract, TokenID: in.TokenID, Address: owner.Data.Owner}
	var balance nft.BalanceResult
	if err := call("POST", "/v1/nft/balance", balanceIn, secret, &balance); err != nil {
		return err
	}
	if balance.Status != "succeeded" || (balance.Data.Amount != "0" && balance.Data.Amount != "1") {
		return fmt.Errorf("NFT quantity invalid")
	}
	for _, q := range []struct {
		name string
		args any
	}{{"get_nft_detail", in}, {"get_nft_owner", in}, {"get_nft_balance", balanceIn}} {
		out, err := session.CallTool(ctx, &mcp.CallToolParams{Name: q.name, Arguments: q.args})
		if err != nil {
			return err
		}
		if out.IsError {
			return fmt.Errorf("%s returned tool error", q.name)
		}
		raw, err := json.Marshal(out.StructuredContent)
		if err != nil {
			return err
		}
		var result struct {
			Status string
			Data   struct {
				Contract, Standard string
				TokenID            string `json:"token_id"`
			}
		}
		if json.Unmarshal(raw, &result) != nil || result.Status != "succeeded" || result.Data.Contract != in.Contract || result.Data.TokenID != "1" || result.Data.Standard != "erc721" {
			return fmt.Errorf("%s structured result mismatch", q.name)
		}
	}
	fmt.Printf("NFT Ethereum ERC-721 detail/owner/quantity verified via REST and MCP (metadata=%s).\n", detail.Data.Metadata.Status)
	return nil
}
