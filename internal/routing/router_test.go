package routing

import (
	"0xbot.org/api/internal/data"
	"context"
	"errors"
	"sync"
	"testing"
)

type settingsFake []Setting

func (s settingsFake) List(context.Context) ([]Setting, error) { return append([]Setting{}, s...), nil }

type providerFake struct {
	id    string
	fail  bool
	mu    sync.Mutex
	calls int
}

func (p *providerFake) ID() string                   { return p.id }
func (p *providerFake) Supports(network string) bool { return network == "eip155:1" }
func (p *providerFake) Balance(ctx context.Context, r data.Request) (data.Balance, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.fail {
		return data.Balance{}, data.ErrUnavailable
	}
	return data.Balance{Amount: "1"}, nil
}
func TestFallbackAndExplicitSelection(t *testing.T) {
	a, b := &providerFake{id: "a", fail: true}, &providerFake{id: "b"}
	r := New([]data.Provider{a, b}, settingsFake{{ID: "a", Enabled: true, Priority: 1}, {ID: "b", Enabled: true, Priority: 2}})
	req := data.Request{Address: "0x0000000000000000000000000000000000000000", Network: "eip155:1"}
	_, p, e := r.Balance(context.Background(), req)
	if e != nil || p != "b" {
		t.Fatal("automatic fallback failed")
	}
	req.Provider = "a"
	_, _, e = r.Balance(context.Background(), req)
	if !errors.Is(e, data.ErrUnavailable) || b.calls != 1 {
		t.Fatal("explicit provider must not silently fallback")
	}
	req.Provider = "missing"
	_, _, e = r.Balance(context.Background(), req)
	if !errors.Is(e, data.ErrProvider) {
		t.Fatal("unknown provider accepted")
	}
}
func TestDisabledAndCircuitOpen(t *testing.T) {
	p := &providerFake{id: "a", fail: true}
	req := data.Request{Address: "0x0000000000000000000000000000000000000000", Network: "eip155:1"}
	r := New([]data.Provider{p}, settingsFake{{ID: "a", Enabled: false, Priority: 1}})
	r.Balance(context.Background(), req)
	if p.calls != 0 {
		t.Fatal("disabled provider invoked")
	}
	r = New([]data.Provider{p}, settingsFake{{ID: "a", Enabled: true, Priority: 1}})
	for i := 0; i < 5; i++ {
		r.Balance(context.Background(), req)
	}
	if p.calls != 3 || r.Health("a") != "circuit_open" {
		t.Fatal("circuit did not open")
	}
}
func TestConcurrentRouting(t *testing.T) {
	p := &providerFake{id: "a"}
	r := New([]data.Provider{p}, settingsFake{{ID: "a", Enabled: true, Priority: 1}})
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, e := r.Balance(context.Background(), data.Request{Address: "0x0000000000000000000000000000000000000000", Network: "eip155:1"})
			if e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
}

type networkProvider struct {
	*providerFake
	network string
}

func (p networkProvider) Supports(network string) bool { return p.network == network }

func TestNetworkScopedFallbackAndHealth(t *testing.T) {
	eth := networkProvider{&providerFake{id: "eth"}, "eip155:1"}
	bsc := networkProvider{&providerFake{id: "bsc", fail: true}, "eip155:56"}
	backup := networkProvider{&providerFake{id: "bsc-backup"}, "eip155:56"}
	polygon := networkProvider{&providerFake{id: "polygon"}, "eip155:137"}
	r := New([]data.Provider{eth, bsc, backup, polygon}, settingsFake{{"eth", true, 1}, {"bsc", true, 2}, {"bsc-backup", true, 3}, {"polygon", true, 4}})
	req := data.Request{Address: "0x0000000000000000000000000000000000000000", Network: "eip155:56"}
	for i := 0; i < 4; i++ {
		if _, id, err := r.Balance(context.Background(), req); err != nil || id != "bsc-backup" {
			t.Fatal("network fallback failed")
		}
	}
	if eth.calls != 0 || polygon.calls != 0 || bsc.calls != 3 || r.Health("eth") != "unobserved" {
		t.Fatal("cross-network routing or health contamination")
	}
	req.Provider = "eth"
	if _, _, err := r.Balance(context.Background(), req); !errors.Is(err, data.ErrProvider) {
		t.Fatal("cross-network explicit route accepted")
	}
	req.Provider = "auto"
	req.Network = "eip155:137"
	if _, id, err := r.Balance(context.Background(), req); err != nil || id != "polygon" {
		t.Fatal("polygon route unavailable")
	}
}
