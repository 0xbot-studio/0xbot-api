package httpapi

import (
	"0xbot.org/api/internal/capabilities"
	"0xbot.org/api/internal/config"
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/identity"
	"0xbot.org/api/internal/messaging"
	"0xbot.org/api/internal/rooms"
	"0xbot.org/api/internal/routing"
	"0xbot.org/api/internal/swaps"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"net/http"
	"strings"
	"time"
)

type API struct {
	Config     config.Config
	DB         readiness
	Identity   identityPort
	Keys       keyPort
	Operations operationReader
	Service    balanceUseCase
	Messages   *messaging.Service
	Rooms      *rooms.Service
	Settings   settingsPort
	Router     routerObserver
	MCP        http.Handler
	OAuth      oauthPort
	Spec       []byte
}

func (a *API) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Recoverer, a.boundary)
	if a.OAuth != nil {
		a.oauthRoutes(r)
	}
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		write(w, 200, map[string]string{"status": "ok", "service": "0xbot-api"})
	})
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if e := a.DB.Ping(r.Context()); e != nil {
			fail(w, r, 503, "not_ready")
			return
		}
		write(w, 200, map[string]string{"status": "ready"})
	})
	r.Get("/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(a.Spec)
	})
	r.Get("/v1/capabilities", a.capabilities)
	r.Get("/v1/providers", a.providers)
	r.Get("/v1/tokens", func(w http.ResponseWriter, r *http.Request) { write(w, 200, data.Tokens()) })
	r.Route("/v1/auth", func(r chi.Router) {
		r.Use(a.authLimit)
		r.Get("/options", a.authOptions)
		r.Post("/wallet/challenge", a.challenge)
		r.Post("/wallet/verify", a.verify)
		r.Get("/x/start", a.xStart)
		r.Get("/x/callback", a.xCallback)
		r.With(a.user).Get("/me", a.me)
		r.With(a.user).Post("/logout", a.logout)
		r.With(a.user).Post("/mfa", a.mfa)
	})
	r.Group(func(r chi.Router) {
		r.Use(a.user)
		r.Get("/v1/keys", a.listKeys)
		r.Post("/v1/keys", a.createKey)
		r.Delete("/v1/keys/{id}", a.revokeKey)
		r.Get("/v1/operations", a.listOperations)
		r.Get("/v1/usage", a.usage)
	})
	if a.Messages != nil {
		r.With(a.access).Post("/v1/messages/send", a.sendMessage)
		r.With(a.access).Post("/v1/messages/list", a.listMessages)
		r.With(a.access).Post("/v1/messages/get", a.getMessage)
	}
	if a.Rooms != nil {
		a.roomRoutes(r)
	}
	r.With(a.access).Post("/v1/data/balance", a.balance)
	r.With(a.access).Post("/v1/data/token-balance", a.tokenBalance)
	r.With(a.access).Post("/v1/data/balances", a.balances)
	r.With(a.access).Post("/v1/nft/token", a.nftDetail)
	r.With(a.access).Post("/v1/prices/asset", a.assetPrice)
	r.With(a.access).Post("/v1/swaps/tokens", a.swapTokens)
	r.With(a.access).Post("/v1/swaps/quote", a.swapQuote)
	r.With(a.access).Post("/v1/swaps/status", a.swapStatus)

	r.With(a.access).Post("/v1/prices/token", a.tokenPrice)
	r.With(a.access).Post("/v1/nft/owner", a.nftOwner)
	r.With(a.access).Post("/v1/nft/balance", a.nftBalance)
	r.With(a.access).Post("/v1/data/token-bound-account/resolve", a.resolveTokenBoundAccount)
	r.With(a.access).Post("/v1/data/token-bound-account/get", a.getTokenBoundAccount)
	r.With(a.access).Handle("/mcp", a.MCP)
	r.Group(func(r chi.Router) {
		r.Use(a.admin)
		r.Get("/v1/admin/providers", a.providers)
		r.Get("/v1/admin/contracts", a.contracts)
		r.Patch("/v1/admin/providers/{id}", a.updateProvider)
		r.Get("/v1/admin/audit", a.audit)
	})
	return r
}
func (a *API) capabilities(w http.ResponseWriter, r *http.Request) {
	out := []map[string]any{}
	for _, c := range capabilities.All() {
		networks := data.NetworkIDs()
		if strings.HasPrefix(c.ID, "messaging.") {
			networks = []string{"eip155:1"}
		}
		if c.ID == "market.asset_price" || strings.HasPrefix(c.ID, "rooms.") {
			networks = []string{}
		}
		status := "preview"
		if strings.HasPrefix(c.ID, "swaps.") {
			networks = swaps.Networks()
			if a.Config.LIFIKey == "" && !a.Config.LIFIPublicAccess {
				status = "planned"
			}
		}
		if c.Scope == "admin:session" {
			status = "deprecated"
		}
		out = append(out, map[string]any{"id": c.ID, "status": status, "networks": networks, "description": c.Description["en"], "fee_usd": "0"})
	}
	for _, id := range []string{"payments.prepare", "nft.owned", "nft.transfers"} {
		out = append(out, map[string]any{"id": id, "status": "planned", "networks": []string{}})
	}
	write(w, 200, out)
}
func (a *API) providers(w http.ResponseWriter, r *http.Request) {
	settings, e := a.Settings.List(r.Context())
	if e != nil {
		respond(w, r, nil, e)
		return
	}
	out := []map[string]any{}
	for _, s := range settings {
		if !a.Router.Knows(s.ID) {
			continue
		}
		meta := a.Router.Describe(s.ID)
		out = append(out, map[string]any{"id": s.ID, "name": meta.Name, "website": meta.Website, "enabled": s.Enabled, "priority": s.Priority, "health": a.Router.Health(s.ID), "capabilities": meta.Capabilities, "networks": meta.Networks, "tier": "public-preview", "pricing_model": "free-public", "upstream_fee_usd": "0", "platform_fee_usd": "0", "requires_api_key": s.ID == "lifi" && !a.Config.LIFIPublicAccess})
	}
	write(w, 200, out)
}
func (a *API) balance(w http.ResponseWriter, r *http.Request) {
	var in data.Request
	if !decode(w, r, &in) {
		return
	}
	out, e := a.Service.Balance(r.Context(), r.Context().Value(workspaceKey).(string), in)
	respond(w, r, out, e)
}
func (a *API) tokenBalance(w http.ResponseWriter, r *http.Request) {
	var in data.TokenRequest
	if !decode(w, r, &in) {
		return
	}
	out, e := a.Service.TokenBalance(r.Context(), WorkspaceFromContext(r.Context()), in)
	respond(w, r, out, e)
}
func (a *API) balances(w http.ResponseWriter, r *http.Request) {
	var in data.Request
	if !decode(w, r, &in) {
		return
	}
	out, e := a.Service.Balances(r.Context(), WorkspaceFromContext(r.Context()), in)
	if e == data.ErrRateLimited {
		w.Header().Set("Retry-After", "60")
		fail(w, r, 429, "rate_limited")
		return
	}
	respond(w, r, out, e)
}
func (a *API) listKeys(w http.ResponseWriter, r *http.Request) {
	v, e := a.Keys.List(r.Context(), r.Context().Value(workspaceKey).(string))
	respond(w, r, v, e)
}
func (a *API) createKey(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if len(in.Name) < 1 || len(in.Name) > 80 {
		fail(w, r, 400, "invalid_name")
		return
	}
	wID := r.Context().Value(workspaceKey).(string)
	ok, e := a.Identity.Allow(r.Context(), "key-create:"+wID, 10, time.Hour)
	if e != nil || !ok {
		fail(w, r, 429, "rate_limited")
		return
	}
	v, e := a.Keys.Create(r.Context(), wID, in.Name)
	respond(w, r, v, e)
}
func (a *API) revokeKey(w http.ResponseWriter, r *http.Request) {
	e := a.Keys.Revoke(r.Context(), r.Context().Value(workspaceKey).(string), chi.URLParam(r, "id"))
	if e != nil {
		fail(w, r, 404, "key_not_found")
		return
	}
	write(w, 200, map[string]bool{"ok": true})
}
func (a *API) listOperations(w http.ResponseWriter, r *http.Request) {
	limit, offset := page(r)
	v, e := a.Operations.List(r.Context(), r.Context().Value(workspaceKey).(string), limit, offset)
	respond(w, r, v, e)
}
func (a *API) usage(w http.ResponseWriter, r *http.Request) {
	v, e := a.Operations.Usage(r.Context(), r.Context().Value(workspaceKey).(string))
	respond(w, r, v, e)
}
func (a *API) updateProvider(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled  *bool `json:"enabled"`
		Priority int   `json:"priority"`
	}
	if !decode(w, r, &in) {
		return
	}
	id := chi.URLParam(r, "id")
	if !a.Router.Knows(id) || in.Enabled == nil || in.Priority < 1 || in.Priority > 100 {
		fail(w, r, 400, "invalid_provider_setting")
		return
	}
	s := r.Context().Value(sessionKey).(identity.Session)
	e := a.Settings.Update(r.Context(), s.User.ID, routing.Setting{ID: id, Enabled: *in.Enabled, Priority: in.Priority})
	respond(w, r, map[string]bool{"ok": true}, e)
}
func (a *API) audit(w http.ResponseWriter, r *http.Request) {
	limit, offset := page(r)
	v, e := a.Settings.Audit(r.Context(), limit, offset)
	respond(w, r, v, e)
}

// WorkspaceFromContext is the transport-to-application bridge used by authenticated MCP calls.
func WorkspaceFromContext(ctx interface{ Value(any) any }) string {
	v, _ := ctx.Value(workspaceKey).(string)
	return v
}
