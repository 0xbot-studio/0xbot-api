package httpapi

import (
	"0xbot.org/api/internal/mcpoauth"
	"0xbot.org/api/internal/platform/token"
	"context"
	"encoding/json"
	"errors"
	"github.com/go-chi/chi/v5"
	"io"
	"mime"
	"net/http"
	"strings"
)

func (a *API) oauthRoutes(r chi.Router) {
	r.Get("/.well-known/oauth-protected-resource", a.resourceMetadata)
	r.Get("/.well-known/oauth-protected-resource/mcp", a.resourceMetadata)
	r.Get("/.well-known/oauth-authorization-server", a.authorizationMetadata)
	r.Route("/oauth", func(r chi.Router) {
		r.Use(a.authLimit)
		r.Post("/register", a.oauthRegister)
		r.Get("/authorize", a.oauthAuthorize)
		r.Post("/token", a.oauthToken)
		r.Post("/revoke", a.oauthRevoke)
	})
	r.Group(func(r chi.Router) {
		r.Use(a.user)
		r.Get("/v1/oauth/requests/{id}", a.oauthConsent)
		r.Post("/v1/oauth/requests/{id}", a.oauthDecide)
		r.Get("/v1/oauth/grants", a.oauthGrants)
		r.Delete("/v1/oauth/grants/{id}", a.oauthRevokeGrant)
	})
}
func (a *API) resourceMetadata(w http.ResponseWriter, r *http.Request) {
	write(w, 200, map[string]any{"resource": a.Config.APIOrigin + "/mcp", "authorization_servers": []string{a.Config.APIOrigin}, "scopes_supported": mcpoauth.SupportedScopes, "bearer_methods_supported": []string{"header"}, "resource_name": "0xbot MCP"})
}
func (a *API) authorizationMetadata(w http.ResponseWriter, r *http.Request) {
	origin := a.Config.APIOrigin
	write(w, 200, map[string]any{"issuer": origin, "authorization_endpoint": origin + "/oauth/authorize", "token_endpoint": origin + "/oauth/token", "registration_endpoint": origin + "/oauth/register", "revocation_endpoint": origin + "/oauth/revoke", "response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code", "refresh_token"}, "code_challenge_methods_supported": []string{"S256"}, "token_endpoint_auth_methods_supported": []string{"none"}, "revocation_endpoint_auth_methods_supported": []string{"none"}, "scopes_supported": mcpoauth.SupportedScopes, "authorization_response_iss_parameter_supported": true, "client_id_metadata_document_supported": false})
}
func oauthError(w http.ResponseWriter, e error) {
	code, status := "server_error", 500
	if errors.Is(e, mcpoauth.ErrRequest) {
		code, status = "invalid_request", 400
	}
	if errors.Is(e, mcpoauth.ErrClient) {
		code, status = "invalid_client", 400
	}
	if errors.Is(e, mcpoauth.ErrGrant) {
		code, status = "invalid_grant", 400
	}
	write(w, status, map[string]string{"error": code})
}
func (a *API) oauthRegister(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	var in mcpoauth.Registration
	d := json.NewDecoder(r.Body)
	if d.Decode(&in) != nil {
		oauthError(w, mcpoauth.ErrRequest)
		return
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		oauthError(w, mcpoauth.ErrRequest)
		return
	}
	c, e := a.OAuth.Register(r.Context(), in)
	if e != nil {
		oauthError(w, e)
		return
	}
	write(w, 201, c)
}
func (a *API) oauthAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	for _, v := range q {
		if len(v) != 1 {
			oauthError(w, mcpoauth.ErrRequest)
			return
		}
	}
	browser := token.New()
	id, e := a.OAuth.Begin(r.Context(), mcpoauth.Authorization{ClientID: q.Get("client_id"), RedirectURI: q.Get("redirect_uri"), State: q.Get("state"), Challenge: q.Get("code_challenge"), Resource: q.Get("resource"), Scope: q.Get("scope"), Method: q.Get("code_challenge_method"), ResponseType: q.Get("response_type")}, browser)
	if e != nil {
		oauthError(w, e)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: a.flowCookieName(), Value: id + "." + browser, Path: "/", MaxAge: 600, HttpOnly: true, Secure: a.Config.Secure, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, a.Config.AppOrigin+"/#oauth/"+id, http.StatusFound)
}
func (a *API) flowCookie(r *http.Request) (string, string) {
	c, e := r.Cookie(a.flowCookieName())
	if e != nil {
		return "", ""
	}
	id, browser, ok := strings.Cut(c.Value, ".")
	if !ok || len(id) != 64 || len(browser) != 64 {
		return "", ""
	}
	return id, browser
}
func (a *API) oauthConsent(w http.ResponseWriter, r *http.Request) {
	id, browser := a.flowCookie(r)
	if id != chi.URLParam(r, "id") {
		fail(w, r, 400, "authorization_expired")
		return
	}
	c, e := a.OAuth.Consent(r.Context(), id, browser)
	if e != nil {
		fail(w, r, 400, "authorization_expired")
		return
	}
	write(w, 200, c)
}
func (a *API) oauthDecide(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != a.Config.AppOrigin {
		fail(w, r, 403, "origin_not_allowed")
		return
	}
	var in struct {
		Approve *bool  `json:"approve"`
		Wallet  string `json:"wallet,omitempty"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Approve == nil {
		fail(w, r, 400, "invalid_request")
		return
	}
	id, browser := a.flowCookie(r)
	if id != chi.URLParam(r, "id") {
		fail(w, r, 400, "authorization_expired")
		return
	}
	var redirect string
	var e error
	if decider, ok := a.OAuth.(interface {
		DecideWallet(context.Context, string, string, string, bool, string) (string, error)
	}); ok {
		redirect, e = decider.DecideWallet(r.Context(), id, browser, WorkspaceFromContext(r.Context()), *in.Approve, in.Wallet)
	} else {
		redirect, e = a.OAuth.Decide(r.Context(), id, browser, WorkspaceFromContext(r.Context()), *in.Approve)
	}
	if e != nil {
		fail(w, r, 400, "authorization_expired")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: a.flowCookieName(), Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: a.Config.Secure, SameSite: http.SameSiteLaxMode})
	write(w, 200, map[string]string{"redirect_uri": redirect})
}
func oauthForm(w http.ResponseWriter, r *http.Request) bool {
	typ, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || typ != "application/x-www-form-urlencoded" || r.URL.RawQuery != "" {
		oauthError(w, mcpoauth.ErrRequest)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if r.ParseForm() != nil {
		oauthError(w, mcpoauth.ErrRequest)
		return false
	}
	for _, v := range r.PostForm {
		if len(v) != 1 {
			oauthError(w, mcpoauth.ErrRequest)
			return false
		}
	}
	// Public clients only. Never silently downgrade an attempted client-secret authentication.
	if r.Header.Get("Authorization") != "" || r.PostForm.Get("client_secret") != "" {
		oauthError(w, mcpoauth.ErrClient)
		return false
	}
	return true
}
func (a *API) oauthToken(w http.ResponseWriter, r *http.Request) {
	if !oauthForm(w, r) {
		return
	}
	q := r.PostForm
	in := mcpoauth.Exchange{ClientID: q.Get("client_id"), Code: q.Get("code"), RedirectURI: q.Get("redirect_uri"), Verifier: q.Get("code_verifier"), Resource: q.Get("resource"), RefreshToken: q.Get("refresh_token"), Scope: q.Get("scope")}
	var out mcpoauth.Tokens
	var e error
	switch q.Get("grant_type") {
	case "authorization_code":
		out, e = a.OAuth.ExchangeCode(r.Context(), in)
	case "refresh_token":
		out, e = a.OAuth.Refresh(r.Context(), in)
	default:
		write(w, 400, map[string]string{"error": "unsupported_grant_type"})
		return
	}
	if e != nil {
		oauthError(w, e)
		return
	}
	w.Header().Set("Pragma", "no-cache")
	write(w, 200, out)
}
func (a *API) oauthRevoke(w http.ResponseWriter, r *http.Request) {
	if !oauthForm(w, r) {
		return
	}
	if r.PostForm.Get("client_id") == "" || r.PostForm.Get("token") == "" {
		oauthError(w, mcpoauth.ErrRequest)
		return
	}
	if e := a.OAuth.RevokeToken(r.Context(), r.PostForm.Get("client_id"), r.PostForm.Get("token")); e != nil {
		oauthError(w, e)
		return
	}
	w.WriteHeader(200)
}
func (a *API) oauthGrants(w http.ResponseWriter, r *http.Request) {
	out, e := a.OAuth.List(r.Context(), WorkspaceFromContext(r.Context()))
	respond(w, r, out, e)
}
func (a *API) oauthRevokeGrant(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != a.Config.AppOrigin {
		fail(w, r, 403, "origin_not_allowed")
		return
	}
	if e := a.OAuth.RevokeGrant(r.Context(), WorkspaceFromContext(r.Context()), chi.URLParam(r, "id")); e != nil {
		fail(w, r, 404, "authorization_not_found")
		return
	}
	write(w, 200, map[string]bool{"ok": true})
}
func publicOAuthPath(path string) bool {
	return path == "/oauth/register" || path == "/oauth/token" || path == "/oauth/revoke" || strings.HasPrefix(path, "/.well-known/oauth-")
}
func (a *API) oauthResume(r *http.Request, origin string) string {
	if a.OAuth == nil || origin != a.Config.AppOrigin {
		return ""
	}
	id, browser := a.flowCookie(r)
	if _, e := a.OAuth.Consent(r.Context(), id, browser); e != nil {
		return ""
	}
	return "#oauth/" + id
}
func (a *API) mcpChallenge(w http.ResponseWriter) {
	if a.OAuth != nil {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+a.Config.APIOrigin+`/.well-known/oauth-protected-resource/mcp", scope="data:read"`)
	}
}

func (a *API) flowCookieName() string {
	if a.Config.Secure {
		return "__Host-oxb_mcp_flow"
	}
	return "oxb_mcp_flow"
}
