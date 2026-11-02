package identity

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type xTransport func(*http.Request) (*http.Response, error)

func (f xTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func xResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestXExchangeRequestContract(t *testing.T) {
	for _, secret := range []string{"", "test-client-secret"} {
		name := "public"
		if secret != "" {
			name = "confidential"
		}
		t.Run(name, func(t *testing.T) {
			calls := 0
			x := XClient{ID: "test-client", Secret: secret, Callback: "https://api.example.test/v1/auth/x/callback"}
			x.HTTP = &http.Client{Transport: xTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				switch calls {
				case 1:
					if r.Method != http.MethodPost || r.URL.String() != "https://api.x.com/2/oauth2/token" {
						t.Fatal("unexpected token endpoint")
					}
					if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
						t.Fatal("token request is not form encoded")
					}
					if err := r.ParseForm(); err != nil {
						t.Fatal(err)
					}
					for key, value := range map[string]string{"grant_type": "authorization_code", "code": "code+with&symbols", "redirect_uri": x.Callback, "code_verifier": "test-verifier", "client_id": x.ID} {
						if r.PostForm.Get(key) != value {
							t.Fatalf("unexpected form field %s", key)
						}
					}
					id, password, basic := r.BasicAuth()
					if secret == "" && r.Header.Get("Authorization") != "" {
						t.Fatal("public client sent credentials")
					}
					if secret != "" && (!basic || id != x.ID || password != secret) {
						t.Fatal("confidential client authentication missing")
					}
					return xResponse(200, `{"access_token":"test-access-token"}`), nil
				case 2:
					if r.Method != http.MethodGet || r.URL.String() != "https://api.x.com/2/users/me" {
						t.Fatal("unexpected profile endpoint")
					}
					if r.Header.Get("Authorization") != "Bearer test-access-token" {
						t.Fatal("profile request missing bearer token")
					}
					return xResponse(200, `{"data":{"id":"12345","name":"Display name","username":"changeable"}}`), nil
				default:
					t.Fatal("unexpected retry")
					return nil, errors.New("unexpected request")
				}
			})}
			id, display, err := x.Exchange(context.Background(), "code+with&symbols", "test-verifier")
			if err != nil || id != "12345" || display != "Display name" || calls != 2 {
				t.Fatalf("exchange result: %q %q %v; calls=%d", id, display, err, calls)
			}
		})
	}
}

func TestXExchangeRejectsUpstreamFailures(t *testing.T) {
	for _, tc := range []struct {
		name      string
		profile   bool
		status    int
		body      string
		transport bool
	}{
		{"token_transport", false, 0, "", true},
		{"token_denied", false, 401, "sensitive-upstream-detail", false},
		{"token_malformed", false, 200, "{", false},
		{"token_empty", false, 200, `{"access_token":""}`, false},
		{"token_oversized", false, 200, `{"access_token":"` + strings.Repeat("x", 65<<10) + `"}`, false},
		{"profile_transport", true, 0, "", true},
		{"profile_denied", true, 403, "sensitive-upstream-detail", false},
		{"profile_malformed", true, 200, "{", false},
		{"profile_missing_id", true, 200, `{"data":{"name":"not-an-identity"}}`, false},
		{"profile_oversized", true, 200, `{"data":{"name":"` + strings.Repeat("x", 65<<10) + `","id":"1"}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			x := XClient{HTTP: &http.Client{Transport: xTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if tc.profile && calls == 1 {
					return xResponse(200, `{"access_token":"test-access-token"}`), nil
				}
				if tc.transport {
					return nil, errors.New("sensitive-upstream-detail")
				}
				return xResponse(tc.status, tc.body), nil
			})}}
			id, name, err := x.Exchange(context.Background(), "test-code", "test-verifier")
			if err == nil || id != "" || name != "" {
				t.Fatal("upstream failure authenticated an identity")
			}
			if strings.Contains(err.Error(), "sensitive-upstream-detail") {
				t.Fatal("upstream details leaked")
			}
			want := 1
			if tc.profile {
				want = 2
			}
			if calls != want {
				t.Fatalf("calls=%d, want %d", calls, want)
			}
		})
	}
}

func TestXExchangePropagatesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	x := XClient{HTTP: &http.Client{Transport: xTransport(func(r *http.Request) (*http.Response, error) {
		if !errors.Is(r.Context().Err(), context.Canceled) {
			t.Fatal("request lost cancellation")
		}
		return nil, r.Context().Err()
	})}}
	if _, _, err := x.Exchange(ctx, "test-code", "test-verifier"); !errors.Is(err, ErrAuth) {
		t.Fatalf("unexpected error %v", err)
	}
}
