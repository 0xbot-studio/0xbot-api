package messaging

import (
	"0xbot.org/api/internal/access"
	"0xbot.org/api/internal/data"
	"context"
	"testing"
	"time"
)

type guards struct{ owns, enabled, allowed bool }

func (g guards) OwnsWallet(context.Context, string, string) (bool, error) { return g.owns, nil }
func (g guards) Enabled(context.Context, string) (bool, error)            { return g.enabled, nil }
func (g guards) Allow(context.Context, string, int, time.Duration) (bool, error) {
	return g.allowed, nil
}

type spyProvider struct{ calls int }

func (p *spyProvider) ID() string { return ProviderID }
func (p *spyProvider) Send(context.Context, string, SendInput) (Message, error) {
	p.calls++
	return Message{}, nil
}
func (p *spyProvider) List(context.Context, ListInput) (Page, error) {
	p.calls++
	return Page{Messages: []Summary{}}, nil
}
func (p *spyProvider) Get(context.Context, GetInput) (Message, error) {
	p.calls++
	return Message{}, nil
}
func TestAuthorityAndAvailability(t *testing.T) {
	wallet := "0x1111111111111111111111111111111111111111"
	for _, tc := range []struct {
		p    access.Principal
		g    guards
		want error
	}{
		{access.Principal{}, guards{true, true, true}, ErrPermission},
		{access.Principal{Workspace: "w", Scope: "data:read"}, guards{true, true, true}, ErrPermission},
		{access.Principal{Workspace: "w", Scope: "messages:read"}, guards{false, true, true}, ErrPermission},
		{access.Principal{Workspace: "w", Scope: "messages:read", Wallet: "0x2222222222222222222222222222222222222222"}, guards{true, true, true}, ErrPermission},
		{access.Principal{Workspace: "w", Scope: "messages:read"}, guards{true, false, true}, data.ErrUnavailable},
		{access.Principal{Workspace: "w", Scope: "messages:read"}, guards{true, true, false}, data.ErrRateLimited},
	} {
		p := &spyProvider{}
		s := Service{Provider: p, Identity: tc.g, Availability: tc.g, Limiter: tc.g}
		out, e := s.List(access.With(context.Background(), tc.p), ListInput{Address: wallet})
		if e != tc.want || p.calls != 0 || out.ID != "" {
			t.Fatal("guard bypass", e, p.calls)
		}
	}
}
