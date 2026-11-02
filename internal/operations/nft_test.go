package operations

import (
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/nft"
	"context"
	"errors"
	"testing"
)

type nftRouterFake struct{ testRouter }

func (r *nftRouterFake) NFTDetail(context.Context, nft.TokenRequest) (nft.Detail, string, error) {
	r.calls++
	return nft.Detail{TokenID: "1"}, "test", r.err
}
func (r *nftRouterFake) NFTOwner(context.Context, nft.TokenRequest) (nft.Owner, string, error) {
	r.calls++
	return nft.Owner{TokenID: "1"}, "test", r.err
}
func (r *nftRouterFake) NFTBalance(context.Context, nft.BalanceRequest) (nft.Balance, string, error) {
	r.calls++
	return nft.Balance{TokenID: "1"}, "test", r.err
}
func TestNFTSharedQuotaAndTenantRecords(t *testing.T) {
	router, recorder := &nftRouterFake{}, &testRecorder{}
	s := New(router, recorder, &testLimiter{t, map[string]int{}})
	in := nft.TokenRequest{Network: "eip155:1", Contract: "0x0000000000000000000000000000000000000001", TokenID: "1"}
	b := nft.BalanceRequest{Network: in.Network, Contract: in.Contract, TokenID: in.TokenID, Address: in.Contract}
	bad := in
	bad.TokenID = "1e5"
	if _, err := s.NFTDetail(context.Background(), "a", bad); !errors.Is(err, data.ErrInput) || router.calls != 0 {
		t.Fatal("invalid input reached router")
	}
	if _, err := s.NFTOwner(context.Background(), "", in); !errors.Is(err, data.ErrInput) {
		t.Fatal("missing tenant accepted")
	}
	for i := 0; i < 20; i++ {
		var err error
		switch i % 3 {
		case 0:
			_, err = s.NFTDetail(context.Background(), "a", in)
		case 1:
			_, err = s.NFTOwner(context.Background(), "a", in)
		case 2:
			_, err = s.NFTBalance(context.Background(), "a", b)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.NFTBalance(context.Background(), "a", b); !errors.Is(err, data.ErrRateLimited) {
		t.Fatal("quota not shared")
	}
	if router.calls != 20 || len(recorder.records) != 20 {
		t.Fatal("incorrect metering")
	}
	router.err = nft.ErrReverted
	out, err := s.NFTOwner(context.Background(), "b", in)
	if !errors.Is(err, nft.ErrReverted) || out.Data.TokenID != "" {
		t.Fatal("partial output")
	}
	last := recorder.records[20]
	if last.WorkspaceID != "b" || last.Capability != "nft.owner" || last.ErrorCode != "nft_query_reverted" || last.Status != "failed" {
		t.Fatal(last)
	}
}
