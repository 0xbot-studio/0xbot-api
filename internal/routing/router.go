package routing

import (
	"0xbot.org/api/internal/capabilities"
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/nft"
	"0xbot.org/api/internal/prices"
	"0xbot.org/api/internal/tokenbound"
	"context"
	"errors"
	"slices"
	"sort"
	"sync"
	"time"
)

type Setting struct {
	ID       string `json:"id"`
	Enabled  bool   `json:"enabled"`
	Priority int    `json:"priority"`
}
type Settings interface {
	List(context.Context) ([]Setting, error)
}
type health struct {
	Failures  int
	OpenUntil time.Time
}
type Router struct {
	providers map[string]data.Provider
	metadata  map[string]Metadata
	settings  Settings
	mu        sync.Mutex
	health    map[string]health
	timeout   time.Duration
}

type Metadata struct {
	Name, Website, Network string
	Capabilities           []string
	Networks               []string
}

func New(ps []data.Provider, s Settings, catalogs ...map[string]Metadata) *Router {
	m := map[string]data.Provider{}
	for _, p := range ps {
		m[p.ID()] = p
	}
	metadata := map[string]Metadata{}
	for _, catalog := range catalogs {
		for id, meta := range catalog {
			metadata[id] = meta
		}
	}
	for id, p := range m {
		meta := metadata[id]
		meta.Capabilities = []string{capabilities.NativeBalance}
		if declared, ok := p.(interface{ CapabilityIDs() []string }); ok {
			meta.Capabilities = append([]string(nil), declared.CapabilityIDs()...)
		}
		if meta.Name == "" {
			meta.Name = id
		}
		for _, n := range data.Networks() {
			if meta.Network != "" {
				break
			}
			if p.Supports(n.ID) {
				meta.Network = n.ID
				break
			}
		}
		if len(meta.Networks) == 0 {
			meta.Networks = []string{meta.Network}
		}
		metadata[id] = meta
	}
	return &Router{providers: m, metadata: metadata, settings: s, health: map[string]health{}, timeout: 4 * time.Second}
}
func (r *Router) Describe(id string) Metadata {
	if m, ok := r.metadata[id]; ok {
		return m
	}
	return Metadata{Name: id}
}
func (r *Router) Knows(id string) bool { _, ok := r.providers[id]; return ok }
func (r *Router) Balance(ctx context.Context, req data.Request) (data.Balance, string, error) {
	if err := req.Validate(); err != nil {
		return data.Balance{}, "", err
	}
	capability := capabilities.NativeBalance
	if req.Contract != "" {
		capability = capabilities.TokenBalance
	}
	items, provider, err := route(r, ctx, req, capability, r.timeout, func(ctx context.Context, p data.Provider) ([]data.Balance, error) {
		reader, ok := p.(interface {
			Balance(context.Context, data.Request) (data.Balance, error)
		})
		if !ok {
			return nil, data.ErrUnavailable
		}
		b, e := reader.Balance(ctx, req)
		if e != nil {
			return nil, e
		}
		return []data.Balance{b}, nil
	})
	if err != nil {
		return data.Balance{}, provider, err
	}
	return items[0], provider, nil
}

func (r *Router) Balances(ctx context.Context, req data.Request) ([]data.Balance, string, error) {
	if err := req.Validate(); err != nil || req.Contract != "" {
		return nil, "", data.ErrInput
	}
	return route(r, ctx, req, capabilities.CommonBalances, 8*time.Second, func(ctx context.Context, p data.Provider) ([]data.Balance, error) {
		batch, ok := p.(data.BatchProvider)
		if !ok {
			return nil, data.ErrUnavailable
		}
		return batch.Balances(ctx, req)
	})
}

func route[T any](r *Router, ctx context.Context, req data.Request, capability string, timeout time.Duration, invoke func(context.Context, data.Provider) (T, error)) (T, string, error) {
	var zero T
	if e := req.Validate(); e != nil {
		return zero, "", e
	}
	return routeValidated(r, ctx, req, capability, timeout, invoke)
}

// Callers validate capability-specific inputs before entering shared routing.
func routeValidated[T any](r *Router, ctx context.Context, req data.Request, capability string, timeout time.Duration, invoke func(context.Context, data.Provider) (T, error)) (T, string, error) {
	var zero T
	if req.Provider != "auto" {
		if p, ok := r.providers[req.Provider]; !ok || !p.Supports(req.Network) || !slices.Contains(r.Describe(req.Provider).Capabilities, capability) {
			return zero, "", data.ErrProvider
		}
	}
	settings, e := r.settings.List(ctx)
	if e != nil {
		return zero, "", e
	}
	sort.SliceStable(settings, func(i, j int) bool { return settings[i].Priority < settings[j].Priority })
	last := ""
	for _, s := range settings {
		p, ok := r.providers[s.ID]
		if !ok || !s.Enabled || !p.Supports(req.Network) || !slices.Contains(r.Describe(s.ID).Capabilities, capability) || (req.Provider != "auto" && req.Provider != s.ID) {
			continue
		}
		r.mu.Lock()
		h := r.health[s.ID]
		r.mu.Unlock()
		if time.Now().Before(h.OpenUntil) {
			continue
		}
		last = s.ID
		callctx, cancel := context.WithTimeout(ctx, timeout)
		b, err := invoke(callctx, p)
		cancel()
		r.mu.Lock()
		h = r.health[s.ID]
		if err == nil || semanticReadError(err) {
			h = health{}
		} else {
			h.Failures++
			if h.Failures >= 3 {
				h.OpenUntil = time.Now().Add(30 * time.Second)
			}
		}
		r.health[s.ID] = h
		r.mu.Unlock()
		if err == nil {
			return b, s.ID, nil
		}
		if semanticReadError(err) {
			return zero, s.ID, err
		}
		if ctx.Err() != nil {
			return zero, last, ctx.Err()
		}
	}
	return zero, last, data.ErrUnavailable
}
func semanticReadError(err error) bool {
	return errors.Is(err, tokenbound.ErrNotAccount) || errors.Is(err, nft.ErrUnsupported) || errors.Is(err, nft.ErrReverted) || errors.Is(err, prices.ErrNotFound) || errors.Is(err, prices.ErrStale) || errors.Is(err, prices.ErrConfidence) || errors.Is(err, data.ErrRateLimited)
}
func (r *Router) Health(id string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	h, ok := r.health[id]
	if !ok {
		return "unobserved"
	}
	if time.Now().Before(h.OpenUntil) {
		return "circuit_open"
	}
	if h.Failures > 0 {
		return "degraded"
	}
	return "healthy"
}
