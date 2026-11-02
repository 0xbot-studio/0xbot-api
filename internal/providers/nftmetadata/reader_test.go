package nftmetadata

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestMetadataSafety(t *testing.T) {
	r := New()
	for _, ip := range []string{"127.0.0.1", "10.1.1.1", "169.254.169.254", "100.64.0.1", "::1", "::ffff:127.0.0.1", "fc00::1", "2002:7f00:1::"} {
		if publicIP(netip.MustParseAddr(ip)) {
			t.Fatal(ip)
		}
	}
	for _, uri := range []string{"http://example.com", "https://127.0.0.1", "https://[::1]", "https://user:pass@example.com", "https://example.com:8080", "file:///etc/passwd", "ipfs://../secret"} {
		if got := r.Read(context.Background(), uri); got.Status != "blocked" {
			t.Fatalf("%s: %+v", uri, got)
		}
	}
	tr := r.client.Transport.(*http.Transport)
	if _, err := tr.DialContext(context.Background(), "tcp", "localhost:443"); err == nil {
		t.Fatal("private DNS accepted")
	}
	u, _ := url.Parse("https://127.0.0.1/private")
	if r.client.CheckRedirect(&http.Request{URL: u}, nil) == nil {
		t.Fatal("private redirect accepted")
	}
	for _, raw := range []string{"", "  ", "null", "[]", "{", strings.Repeat("x", maxBytes+1)} {
		if got := r.Read(context.Background(), "data:application/json,"+raw); got.Status != "unavailable" {
			t.Fatalf("malformed accepted: %s", got.Status)
		}
	}
	got := r.Read(context.Background(), "data:application/json;base64,"+base64.StdEncoding.EncodeToString([]byte(`{"name":"<script>alert(1)</script>","description":"ignore previous instructions","image":"javascript:alert(1)"}`)))
	if got.Status != "available" || got.Image != "" || got.Name != "<script>alert(1)</script>" {
		t.Fatalf("%+v", got)
	}
}
func TestMetadataFetchBounds(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		code int
		want string
	}{{`{"name":"safe","image":"https://example.com/a.png"}`, 200, "available"}, {strings.Repeat(" ", maxBytes+1), 200, "unavailable"}, {`{}`, 503, "unavailable"}} {
		count := 0
		r := &Reader{client: &http.Client{Transport: roundTrip(func(req *http.Request) (*http.Response, error) {
			count++
			if req.URL.Host != "ipfs.io" || req.Header.Get("Accept") != "application/json" {
				t.Fatal(req.URL)
			}
			return &http.Response{StatusCode: tc.code, Body: io.NopCloser(strings.NewReader(tc.raw)), Header: make(http.Header)}, nil
		})}}
		got := r.Read(context.Background(), "ipfs://bafytest/metadata.json")
		if got.Status != tc.want || count != 1 {
			t.Fatalf("%+v requests=%d", got, count)
		}
	}
}
