package mcpoauth

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const Scope = "data:read"

var ErrRequest = errors.New("invalid_request")
var ErrClient = errors.New("invalid_client")
var ErrGrant = errors.New("invalid_grant")
var verifierPattern = regexp.MustCompile("^[A-Za-z0-9._~-]{43,128}$")
var challengePattern = regexp.MustCompile("^[A-Za-z0-9_-]{43}$")

type Registration struct {
	Name          string   `json:"client_name"`
	Redirects     []string `json:"redirect_uris"`
	AuthMethod    string   `json:"token_endpoint_auth_method"`
	GrantTypes    []string `json:"grant_types"`
	ResponseTypes []string `json:"response_types"`
}
type Client struct {
	ID            string   `json:"client_id"`
	Name          string   `json:"client_name"`
	Redirects     []string `json:"redirect_uris"`
	AuthMethod    string   `json:"token_endpoint_auth_method"`
	GrantTypes    []string `json:"grant_types"`
	ResponseTypes []string `json:"response_types"`
}
type Authorization struct{ ClientID, RedirectURI, State, Challenge, Resource, Scope, Method, ResponseType string }
type Consent struct {
	ID          string `json:"id"`
	ClientID    string `json:"client_id"`
	ClientName  string `json:"client_name"`
	RedirectURI string `json:"redirect_uri"`
	Resource    string `json:"resource"`
	Scope       string `json:"scope"`
}
type Exchange struct{ ClientID, Code, RedirectURI, Verifier, Resource, RefreshToken, Scope string }
type Tokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}
type Grant struct {
	Wallet     string    `json:"wallet"`
	ID         string    `json:"id"`
	ClientName string    `json:"client_name"`
	Scope      string    `json:"scope"`
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at"`
}

func Challenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}
func validRedirect(raw string) bool {
	if len(raw) > 2048 || strings.ContainsAny(raw, "\r\n") {
		return false
	}
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" || strings.Contains(u.Host, "*") {
		return false
	}
	for _, key := range []string{"code", "error", "state", "iss"} {
		if u.Query().Has(key) {
			return false
		}
	}
	if u.Scheme == "https" {
		return true
	}
	return u.Scheme == "http" && loopback(u.Hostname())
}
func loopback(host string) bool {
	ip := net.ParseIP(host)
	return host == "localhost" || (ip != nil && ip.IsLoopback())
}
func redirectMatches(registered, actual string) bool {
	if !validRedirect(actual) {
		return false
	}
	if registered == actual {
		return true
	}
	a, e := url.Parse(registered)
	b, f := url.Parse(actual)
	if e != nil || f != nil || a.Scheme != "http" || b.Scheme != "http" || !loopback(a.Hostname()) || a.Hostname() != b.Hostname() {
		return false
	}
	// RFC 8252: only a loopback port may vary, never host/path/query.
	a.Host = a.Hostname()
	b.Host = b.Hostname()
	return a.String() == b.String()
}
func validateRegistration(r *Registration) (bool, error) {
	r.Name = strings.TrimSpace(r.Name)
	if r.Name == "" {
		r.Name = "MCP client"
	}
	if len(r.Name) > 80 || len(r.Redirects) < 1 || len(r.Redirects) > 5 || (r.AuthMethod != "" && r.AuthMethod != "none") {
		return false, ErrRequest
	}
	for _, redirect := range r.Redirects {
		if !validRedirect(redirect) {
			return false, ErrRequest
		}
	}
	if len(r.GrantTypes) == 0 {
		r.GrantTypes = []string{"authorization_code", "refresh_token"}
	}
	if len(r.ResponseTypes) == 0 {
		r.ResponseTypes = []string{"code"}
	}
	if len(r.ResponseTypes) != 1 || r.ResponseTypes[0] != "code" {
		return false, ErrRequest
	}
	refresh, code := false, false
	for _, g := range r.GrantTypes {
		switch g {
		case "authorization_code":
			code = true
		case "refresh_token":
			refresh = true
		default:
			return false, ErrRequest
		}
	}
	if !code {
		return false, ErrRequest
	}
	return refresh, nil
}
