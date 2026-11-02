package routing

import (
	"0xbot.org/api/internal/capabilities"
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/tokenbound"
	"context"
	"errors"
	"testing"
)

type tokenBoundFake struct {
	*providerFake
	err   error
	reads int
}

func (p *tokenBoundFake) CapabilityIDs() []string {
	return []string{capabilities.ResolveTokenBoundAccount, capabilities.GetTokenBoundAccount}
}
func (p *tokenBoundFake) ResolveTokenBoundAccount(context.Context, tokenbound.ResolveRequest) (tokenbound.Account, error) {
	p.reads++
	return tokenbound.Account{Address: "fixture"}, p.err
}
func (p *tokenBoundFake) GetTokenBoundAccount(context.Context, tokenbound.GetRequest) (tokenbound.Account, error) {
	p.reads++
	return tokenbound.Account{Address: "fixture"}, p.err
}
func TestTokenBoundRouting(t *testing.T) {
	legacy := &providerFake{id: "legacy"}
	a := &tokenBoundFake{providerFake: &providerFake{id: "a"}, err: data.ErrUnavailable}
	b := &tokenBoundFake{providerFake: &providerFake{id: "b"}}
	r := New([]data.Provider{legacy, a, b}, settingsFake{{"legacy", true, 0}, {"a", true, 1}, {"b", true, 2}})
	in := tokenbound.GetRequest{Address: "0x0000000000000000000000000000000000000001", Network: "eip155:1"}
	if _, provider, err := r.GetTokenBoundAccount(context.Background(), in); err != nil || provider != "b" || a.reads != 1 || legacy.calls != 0 {
		t.Fatal("capability filtering/fallback failed")
	}
	in.Provider = "legacy"
	if _, _, err := r.GetTokenBoundAccount(context.Background(), in); !errors.Is(err, data.ErrProvider) {
		t.Fatal("unsupported capability accepted")
	}
	in.Provider = "a"
	if _, _, err := r.GetTokenBoundAccount(context.Background(), in); !errors.Is(err, data.ErrUnavailable) || b.reads != 1 {
		t.Fatal("explicit selection fell back")
	}
	in.Provider = "auto"
	a.err = tokenbound.ErrNotAccount
	for i := 0; i < 4; i++ {
		if _, _, err := r.GetTokenBoundAccount(context.Background(), in); !errors.Is(err, tokenbound.ErrNotAccount) {
			t.Fatal("semantic error lost")
		}
	}
	if b.reads != 1 || r.Health("a") != "healthy" {
		t.Fatal("non-account result must not trip circuit or cause fallback")
	}
	in.Network = "eip155:56"
	if _, _, err := r.GetTokenBoundAccount(context.Background(), in); !errors.Is(err, data.ErrUnavailable) {
		t.Fatal("wrong network routed")
	}
}
