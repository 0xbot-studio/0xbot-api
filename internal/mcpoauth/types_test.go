package mcpoauth

import "testing"

func TestRedirectAndPKCEPolicy(t *testing.T) {
	for _, u := range []string{"javascript:alert(1)", "http://example.com/callback", "https://good.test/cb#fragment", "https://user:pass@good.test/cb", "https://*.test/cb", "file:///tmp/a"} {
		if validRedirect(u) {
			t.Errorf("accepted unsafe redirect %q", u)
		}
	}
	for _, u := range []string{"https://app.test/callback", "http://127.0.0.1:5678/callback", "http://localhost:8787/callback", "http://[::1]:8000/cb"} {
		if !validRedirect(u) {
			t.Errorf("rejected valid redirect %q", u)
		}
	}
	if !redirectMatches("http://127.0.0.1/cb", "http://127.0.0.1:8899/cb") {
		t.Fatal("loopback variable port")
	}
	for _, u := range []string{"http://localhost:8899/cb", "http://127.0.0.1:8899/other", "http://127.0.0.1:8899/cb?evil=1"} {
		if redirectMatches("http://127.0.0.1/cb", u) {
			t.Fatal("mismatched redirect accepted")
		}
	}
	if redirectMatches("https://app.test/cb", "https://app.test:8443/cb") {
		t.Fatal("non-loopback port wildcard")
	}
	if Challenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk") != "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM" {
		t.Fatal("RFC7636 vector")
	}
}
