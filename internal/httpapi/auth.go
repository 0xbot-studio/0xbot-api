package httpapi

import (
	"0xbot.org/api/internal/identity"
	"0xbot.org/api/internal/platform/token"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (a *API) authOptions(w http.ResponseWriter, r *http.Request) {
	write(w, 200, map[string]any{"wallet": true, "x": a.Config.XClientID != "", "wallet_type": "evm_eoa", "registration": "on_first_login"})
}
func (a *API) challenge(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Address string `json:"address"`
		Link    bool   `json:"link"`
	}
	if !decode(w, r, &in) {
		return
	}
	origin := r.Header.Get("Origin")
	if origin != a.Config.AppOrigin && origin != a.Config.AdminOrigin {
		fail(w, r, 403, "origin_required")
		return
	}
	var binding *string
	if in.Link {
		s, e := a.session(r)
		if e != nil || time.Since(s.CreatedAt) > 10*time.Minute {
			fail(w, r, 401, "recent_login_required")
			return
		}
		binding = &s.User.ID
	}
	u, _ := url.Parse(origin)
	c, e := identity.NewChallenge(in.Address, origin, u.Host, binding)
	if e != nil {
		respond(w, r, nil, e)
		return
	}
	e = a.Identity.SaveChallenge(r.Context(), c)
	respond(w, r, map[string]any{"id": c.ID, "message": c.Message, "expires_at": c.Expires}, e)
}
func (a *API) verify(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID        string `json:"id"`
		Signature string `json:"signature"`
	}
	if !decode(w, r, &in) {
		return
	}
	c, e := a.Identity.Challenge(r.Context(), in.ID)
	if e != nil {
		respond(w, r, nil, e)
		return
	}
	if c.Origin != r.Header.Get("Origin") {
		fail(w, r, 403, "origin_not_allowed")
		return
	}
	if c.BindingUser != nil {
		s, e := a.session(r)
		if e != nil || s.User.ID != *c.BindingUser || time.Since(s.CreatedAt) > 10*time.Minute {
			fail(w, r, 401, "recent_login_required")
			return
		}
	}
	if e = identity.VerifyChallenge(c, in.Signature); e != nil {
		respond(w, r, nil, e)
		return
	}
	if e = a.Identity.ConsumeChallenge(r.Context(), c.ID); e != nil {
		respond(w, r, nil, e)
		return
	}
	id, e := a.Identity.FindOrCreate(r.Context(), "wallet", c.Address, c.Address[:6]+"…"+c.Address[len(c.Address)-4:], c.BindingUser)
	if e != nil {
		respond(w, r, nil, e)
		return
	}
	aud := "app"
	if c.Origin == a.Config.AdminOrigin {
		aud = "admin"
	}
	raw, e := a.Identity.CreateSession(r.Context(), id, aud)
	if e != nil {
		respond(w, r, nil, e)
		return
	}
	a.setSession(w, raw, c.Origin)
	write(w, 200, map[string]bool{"authenticated": true})
}
func (a *API) me(w http.ResponseWriter, r *http.Request) {
	s := r.Context().Value(sessionKey).(identity.Session)
	write(w, 200, s.User)
}
func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	s := r.Context().Value(sessionKey).(identity.Session)
	if e := a.Identity.Revoke(r.Context(), s.Hash); e != nil {
		respond(w, r, nil, e)
		return
	}
	name := "oxb_session"
	if s.Audience == "admin" {
		name = "oxb_admin_session"
	}
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: a.Config.Secure, SameSite: http.SameSiteLaxMode})
	write(w, 200, map[string]bool{"ok": true})
}
func (a *API) mfa(w http.ResponseWriter, r *http.Request) {
	s := r.Context().Value(sessionKey).(identity.Session)
	if !s.User.Admin || s.Audience != "admin" {
		fail(w, r, 403, "admin_required")
		return
	}
	var in struct {
		Code string `json:"code"`
	}
	if !decode(w, r, &in) {
		return
	}
	ok, e := a.Identity.Allow(r.Context(), "mfa:"+s.User.ID, 5, time.Minute)
	if e != nil || !ok {
		fail(w, r, 429, "rate_limited")
		return
	}
	if !identity.VerifyTOTP(a.Config.TOTPSecret, in.Code, time.Now()) {
		fail(w, r, 401, "invalid_mfa_code")
		return
	}
	ok, e = a.Identity.Allow(r.Context(), "totp:"+token.Hash(a.Config.TOTPSecret+in.Code), 1, 90*time.Second)
	if e != nil || !ok {
		fail(w, r, 401, "invalid_mfa_code")
		return
	}
	e = a.Identity.MFA(r.Context(), s.Hash)
	respond(w, r, map[string]bool{"ok": true}, e)
}
func (a *API) xStart(w http.ResponseWriter, r *http.Request) {
	if a.Config.XClientID == "" {
		fail(w, r, 503, "x_not_configured")
		return
	}
	origin := r.URL.Query().Get("origin")
	if origin != a.Config.AppOrigin && origin != a.Config.AdminOrigin {
		fail(w, r, 400, "invalid_origin")
		return
	}
	var binding *string
	if r.URL.Query().Get("link") == "true" {
		r.Header.Set("Origin", origin)
		s, e := a.session(r)
		if e != nil || time.Since(s.CreatedAt) > 10*time.Minute {
			fail(w, r, 401, "recent_login_required")
			return
		}
		binding = &s.User.ID
	}
	state, verifier, browser := token.New(), token.New(), token.New()
	v := identity.OAuthState{BrowserHash: token.Hash(browser), Verifier: verifier, Origin: origin, BindingUser: binding}
	if e := a.Identity.SaveOAuth(r.Context(), state, v); e != nil {
		respond(w, r, nil, e)
		return
	}
	h := sha256.Sum256([]byte(verifier))
	values := url.Values{"response_type": {"code"}, "client_id": {a.Config.XClientID}, "redirect_uri": {a.Config.APIOrigin + "/v1/auth/x/callback"}, "scope": {"users.read tweet.read"}, "state": {state}, "code_challenge": {base64.RawURLEncoding.EncodeToString(h[:])}, "code_challenge_method": {"S256"}}
	http.SetCookie(w, &http.Cookie{Name: "oxb_oauth", Value: browser, Path: "/v1/auth/x/callback", HttpOnly: true, Secure: a.Config.Secure, SameSite: http.SameSiteLaxMode, MaxAge: 300})
	http.Redirect(w, r, "https://x.com/i/oauth2/authorize?"+values.Encode(), http.StatusFound)
}
func (a *API) xCallback(w http.ResponseWriter, r *http.Request) {
	cookie, e := r.Cookie("oxb_oauth")
	if e != nil {
		fail(w, r, 401, "authentication_required")
		return
	}
	v, e := a.Identity.ConsumeOAuth(r.Context(), r.URL.Query().Get("state"), cookie.Value)
	if e != nil {
		respond(w, r, nil, e)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "oxb_oauth", Value: "", Path: "/v1/auth/x/callback", HttpOnly: true, Secure: a.Config.Secure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	redirectError := func(code string) {
		http.Redirect(w, r, v.Origin+"/?auth_error="+url.QueryEscape(code)+a.oauthResume(r, v.Origin), http.StatusFound)
	}
	if r.URL.Query().Get("error") != "" {
		redirectError("authorization_cancelled")
		return
	}
	if v.BindingUser != nil {
		r.Header.Set("Origin", v.Origin)
		s, e := a.session(r)
		if e != nil || s.User.ID != *v.BindingUser || time.Since(s.CreatedAt) > 10*time.Minute {
			redirectError("recent_login_required")
			return
		}
	}
	x := identity.XClient{ID: a.Config.XClientID, Secret: a.Config.XClientSecret, Callback: a.Config.APIOrigin + "/v1/auth/x/callback"}
	subject, name, e := x.Exchange(r.Context(), r.URL.Query().Get("code"), v.Verifier)
	if e != nil {
		redirectError("x_login_failed")
		return
	}
	id, e := a.Identity.FindOrCreate(r.Context(), "x", subject, name, v.BindingUser)
	if e != nil {
		redirectError("identity_already_linked")
		return
	}
	aud := "app"
	if v.Origin == a.Config.AdminOrigin {
		aud = "admin"
	}
	raw, e := a.Identity.CreateSession(r.Context(), id, aud)
	if e != nil {
		redirectError("x_login_failed")
		return
	}
	a.setSession(w, raw, v.Origin)
	http.Redirect(w, r, strings.TrimRight(v.Origin, "/")+"/"+a.oauthResume(r, v.Origin), http.StatusFound)
}
