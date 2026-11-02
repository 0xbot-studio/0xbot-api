package httpapi

import (
	"0xbot.org/api/internal/access"
	"0xbot.org/api/internal/identity"
	"0xbot.org/api/internal/platform/token"
	"context"
	"net"
	"net/http"
	"strings"
	"time"
)

type contextKey string

const sessionKey contextKey = "session"
const workspaceKey contextKey = "workspace"

func (a *API) boundary(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allowed := origin == a.Config.AppOrigin || origin == a.Config.AdminOrigin
		protocol := publicOAuthPath(r.URL.Path) || r.URL.Path == "/mcp"
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		if origin != "" && !allowed && !protocol {
			fail(w, r, 403, "origin_not_allowed")
			return
		}
		if allowed {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Mcp-Protocol-Version, Mcp-Session-Id")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Expose-Headers", "Mcp-Session-Id")
		}
		if protocol && !allowed {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Mcp-Protocol-Version, Mcp-Session-Id")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		}
		if protocol {
			w.Header().Set("Access-Control-Expose-Headers", "WWW-Authenticate, Mcp-Session-Id")
		}
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" && origin == "" && r.Header.Get("Authorization") == "" && !protocol {
			fail(w, r, 403, "origin_required")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (a *API) session(r *http.Request) (identity.Session, error) {
	name := "oxb_session"
	if r.Header.Get("Origin") == a.Config.AdminOrigin {
		name = "oxb_admin_session"
	}
	c, e := r.Cookie(name)
	if e != nil {
		return identity.Session{}, identity.ErrAuth
	}
	s, e := a.Identity.Session(r.Context(), c.Value)
	if e != nil {
		return s, e
	}
	audience := "app"
	if name == "oxb_admin_session" {
		audience = "admin"
	}
	if s.Audience != audience {
		return s, identity.ErrAuth
	}
	s.User.Admin = false
	for _, i := range s.User.Identities {
		if i.Kind == "wallet" && a.Config.AdminWallets[i.Subject] {
			s.User.Admin = true
		}
	}
	s.User.MFA = s.AdminVerified != nil && time.Since(*s.AdminVerified) < 30*time.Minute
	return s, nil
}
func (a *API) user(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, e := a.session(r)
		if e != nil {
			fail(w, r, 401, "authentication_required")
			return
		}
		ctx := context.WithValue(r.Context(), sessionKey, s)
		ctx = context.WithValue(ctx, workspaceKey, s.User.WorkspaceID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
func (a *API) admin(next http.Handler) http.Handler {
	return a.user(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := r.Context().Value(sessionKey).(identity.Session)
		if s.Audience != "admin" || !s.User.Admin {
			fail(w, r, 403, "admin_required")
			return
		}
		if !s.User.MFA {
			fail(w, r, 403, "mfa_required")
			return
		}
		next.ServeHTTP(w, r)
	}))
}
func (a *API) access(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var workspace string
		p := access.Principal{Scope: "data:read"}
		if r.URL.Path == "/mcp" {
			a.mcpChallenge(w)
		}
		if raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
			var e error
			if r.URL.Path == "/mcp" && a.OAuth != nil && strings.HasPrefix(raw, "oxbo_at_") {
				if auth, ok := a.OAuth.(interface {
					AuthenticateAccess(context.Context, string) (access.Principal, error)
				}); ok {
					p, e = auth.AuthenticateAccess(r.Context(), raw)
					workspace = p.Workspace
				} else {
					workspace, e = a.OAuth.Authenticate(r.Context(), raw)
				}
			} else {
				workspace, e = a.Keys.Authenticate(r.Context(), raw)
			}
			if e != nil {
				if r.URL.Path == "/mcp" && a.OAuth != nil {
					w.Header().Set("WWW-Authenticate", w.Header().Get("WWW-Authenticate")+`, error="invalid_token"`)
				}
				fail(w, r, 401, "invalid_api_key")
				return
			}
		} else {
			if r.URL.Path == "/mcp" && r.Header.Get("Origin") != a.Config.AppOrigin && r.Header.Get("Origin") != a.Config.AdminOrigin {
				fail(w, r, 401, "authentication_required")
				return
			}
			s, e := a.session(r)
			if e != nil {
				fail(w, r, 401, "authentication_required")
				return
			}
			workspace = s.User.WorkspaceID
			if s.Audience == "app" {
				p.Scope = "data:read messages:read messages:send rooms:read rooms:write"
			}
		}
		ok, e := a.Identity.Allow(r.Context(), "query:"+workspace, 60, time.Minute)
		if e != nil {
			fail(w, r, 503, "service_unavailable")
			return
		}
		if !ok {
			w.Header().Set("Retry-After", "60")
			fail(w, r, 429, "rate_limited")
			return
		}
		p.Workspace = workspace
		next.ServeHTTP(w, r.WithContext(access.With(context.WithValue(r.Context(), workspaceKey, workspace), p)))
	})
}
func (a *API) authLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		peer := net.ParseIP(host)
		if a.Config.TrustProxy && peer != nil && (peer.IsLoopback() || peer.IsPrivate()) {
			if client := net.ParseIP(r.Header.Get("X-Real-IP")); client != nil {
				host = client.String()
			}
		}
		ok, e := a.Identity.Allow(r.Context(), "auth:"+token.Hash(host), 30, time.Minute)
		if e != nil {
			fail(w, r, 503, "service_unavailable")
			return
		}
		if !ok {
			fail(w, r, 429, "rate_limited")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (a *API) setSession(w http.ResponseWriter, raw, origin string) {
	name := "oxb_session"
	if origin == a.Config.AdminOrigin {
		name = "oxb_admin_session"
	}
	http.SetCookie(w, &http.Cookie{Name: name, Value: raw, Path: "/", HttpOnly: true, Secure: a.Config.Secure, SameSite: http.SameSiteLaxMode, MaxAge: 7 * 24 * 3600})
}
