package httpapi

import (
	"0xbot.org/api/internal/config"
	"0xbot.org/api/internal/identity"
	"0xbot.org/api/internal/platform/token"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// Only OAuth persistence is used by these focused handler tests. Real atomic
// consumption and expiry are covered by identity's database integration tests.
type oauthStore struct {
	identityPort
	state    string
	value    identity.OAuthState
	consumed bool
}

func (s *oauthStore) SaveOAuth(_ context.Context, state string, value identity.OAuthState) error {
	s.state, s.value = state, value
	return nil
}

func (s *oauthStore) ConsumeOAuth(_ context.Context, state, browser string) (identity.OAuthState, error) {
	if s.consumed || state != s.state || token.Hash(browser) != s.value.BrowserHash {
		return identity.OAuthState{}, identity.ErrAuth
	}
	s.consumed = true
	return s.value, nil
}

func TestXStartAndCancelledCallback(t *testing.T) {
	s := &oauthStore{}
	a := &API{Config: config.Config{XClientID: "test-client", APIOrigin: "https://api.example.test", AppOrigin: "https://app.example.test", AdminOrigin: "https://admin.example.test", Secure: true}, Identity: s}
	w := httptest.NewRecorder()
	a.xStart(w, httptest.NewRequest("GET", "/v1/auth/x/start?origin="+url.QueryEscape(a.Config.AppOrigin), nil))
	if w.Code != http.StatusFound {
		t.Fatalf("start status %d", w.Code)
	}
	location, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if location.Scheme != "https" || location.Host != "x.com" || location.Path != "/i/oauth2/authorize" {
		t.Fatal("unexpected authorization endpoint")
	}
	hash := sha256.Sum256([]byte(s.value.Verifier))
	for key, want := range map[string]string{"client_id": "test-client", "response_type": "code", "state": s.state, "redirect_uri": a.Config.APIOrigin + "/v1/auth/x/callback", "code_challenge_method": "S256", "code_challenge": base64.RawURLEncoding.EncodeToString(hash[:]), "scope": "users.read tweet.read"} {
		if location.Query().Get(key) != want {
			t.Fatalf("unexpected authorization parameter %s", key)
		}
	}
	if s.state == "" || s.value.Verifier == "" || s.value.Origin != a.Config.AppOrigin {
		t.Fatal("missing state context")
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing browser cookie")
	}
	cookie := cookies[0]
	if cookie.Name != "oxb_oauth" || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/v1/auth/x/callback" || cookie.MaxAge != 300 || cookie.Domain != "" {
		t.Fatal("unsafe browser cookie attributes")
	}
	if token.Hash(cookie.Value) != s.value.BrowserHash {
		t.Fatal("cookie is not bound to state")
	}
	callback := "/v1/auth/x/callback?state=" + url.QueryEscape(s.state) + "&error=access_denied"
	for _, browserCookie := range []*http.Cookie{nil, {Name: "oxb_oauth", Value: "wrong-browser"}} {
		request := httptest.NewRequest("GET", callback, nil)
		if browserCookie != nil {
			request.AddCookie(browserCookie)
		}
		response := httptest.NewRecorder()
		a.xCallback(response, request)
		if response.Code != http.StatusUnauthorized || s.consumed {
			t.Fatal("invalid browser reached callback processing")
		}
	}
	request := httptest.NewRequest("GET", callback, nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	a.xCallback(response, request)
	if response.Code != http.StatusFound || response.Header().Get("Location") != a.Config.AppOrigin+"/?auth_error=authorization_cancelled" {
		t.Fatal("cancellation redirect is incorrect")
	}
	cleared := response.Result().Cookies()
	if len(cleared) != 1 || cleared[0].Name != cookie.Name || cleared[0].MaxAge != -1 || cleared[0].Path != cookie.Path {
		t.Fatal("browser state cookie was not cleared")
	}
	response = httptest.NewRecorder()
	a.xCallback(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatal("callback replay accepted")
	}
}

func TestXStartRejectsUnconfiguredAndUntrustedOrigin(t *testing.T) {
	a := &API{Config: config.Config{AppOrigin: "https://app.example.test"}}
	w := httptest.NewRecorder()
	a.xStart(w, httptest.NewRequest("GET", "/v1/auth/x/start", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal("unconfigured X enabled")
	}
	a.Config.XClientID = "test-client"
	for _, origin := range []string{"https://evil.example.test", "https://app.example.test.evil.test"} {
		w = httptest.NewRecorder()
		a.xStart(w, httptest.NewRequest("GET", "/v1/auth/x/start?origin="+url.QueryEscape(origin), nil))
		if w.Code != http.StatusBadRequest || w.Header().Get("Location") != "" {
			t.Fatal("untrusted origin accepted")
		}
	}
}
