package operations

import (
	"0xbot.org/api/internal/capabilities"
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/tokenbound"
	"context"
	"errors"
	"strings"
	"testing"
)

type accountRouterFake struct{ testRouter }

func (r *accountRouterFake) ResolveTokenBoundAccount(context.Context, tokenbound.ResolveRequest) (tokenbound.Account, string, error) {
	r.calls++
	return tokenbound.Account{Address: "fixture"}, "test", r.err
}
func (r *accountRouterFake) GetTokenBoundAccount(context.Context, tokenbound.GetRequest) (tokenbound.Account, string, error) {
	r.calls++
	return tokenbound.Account{Address: "fixture"}, "test", r.err
}
func TestTokenBoundSharedLimitAndRecords(t *testing.T) {
	router, recorder := &accountRouterFake{}, &testRecorder{}
	service := New(router, recorder, &testLimiter{t, map[string]int{}})
	get := tokenbound.GetRequest{Address: "0x0000000000000000000000000000000000000001", Network: "eip155:1"}
	resolve := tokenbound.ResolveRequest{Network: get.Network, TokenContract: get.Address, TokenID: "0", Implementation: get.Address, Salt: "0x" + strings.Repeat("0", 64)}
	bad := resolve
	bad.Salt = ""
	if _, err := service.ResolveTokenBoundAccount(context.Background(), "a", bad); !errors.Is(err, data.ErrInput) || router.calls != 0 {
		t.Fatal("invalid request reached provider")
	}
	for i := 0; i < 10; i++ {
		if _, err := service.ResolveTokenBoundAccount(context.Background(), "a", resolve); err != nil {
			t.Fatal(err)
		}
		if _, err := service.GetTokenBoundAccount(context.Background(), "a", get); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.GetTokenBoundAccount(context.Background(), "a", get); !errors.Is(err, data.ErrRateLimited) {
		t.Fatal("resolve/get did not share rate limit")
	}
	if len(recorder.records) != 20 || router.calls != 20 {
		t.Fatal("incorrect metering")
	}
	if recorder.records[0].Capability != capabilities.ResolveTokenBoundAccount || recorder.records[1].Capability != capabilities.GetTokenBoundAccount {
		t.Fatal("capability record mismatch")
	}
	router.err = tokenbound.ErrNotAccount
	out, err := service.GetTokenBoundAccount(context.Background(), "b", get)
	if !errors.Is(err, tokenbound.ErrNotAccount) || out.Data.Address != "" {
		t.Fatal("partial failure exposed")
	}
	last := recorder.records[20]
	if last.WorkspaceID != "b" || last.ErrorCode != "not_token_bound_account" || last.Status != "failed" {
		t.Fatal("failure/tenant record mismatch")
	}
}
