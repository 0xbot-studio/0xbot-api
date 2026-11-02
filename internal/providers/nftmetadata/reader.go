// Package nftmetadata fetches untrusted NFT metadata with a private-network-safe
// transport. It never fetches image resources or returns executable markup.
package nftmetadata

import (
	"0xbot.org/api/internal/nft"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const maxBytes = 256 << 10

var errBlocked = errors.New("blocked_metadata_url")

type Reader struct{ client *http.Client }

func New() *Reader {
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	transport := &http.Transport{Proxy: nil, MaxIdleConns: 20, IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 3 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || port != "443" {
			return nil, errBlocked
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, errBlocked
		}
		for _, ip := range ips {
			if !publicIP(ip) {
				return nil, errBlocked
			}
		}
		// Dial the validated literal to prevent DNS rebinding; TLS still verifies host.
		return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
	}
	return &Reader{client: &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errBlocked
		}
		return validURL(req.URL)
	}}}
}
func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, cidr := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "2001::/32", "2002::/16", "64:ff9b::/96"} {
		if netip.MustParsePrefix(cidr).Contains(ip) {
			return false
		}
	}
	return true
}
func validURL(u *url.URL) error {
	if u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") {
		return errBlocked
	}
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil && !publicIP(ip) {
		return errBlocked
	}
	return nil
}
func (r *Reader) Read(ctx context.Context, uri string) nft.Metadata {
	out := nft.Metadata{Status: "unavailable", SourceURI: uri}
	if len(uri) > maxBytes {
		return out
	}
	var raw []byte
	var err error
	switch {
	case strings.HasPrefix(uri, "data:application/json;base64,"):
		raw, err = base64.StdEncoding.DecodeString(strings.TrimPrefix(uri, "data:application/json;base64,"))
	case strings.HasPrefix(uri, "data:application/json,"):
		var decoded string
		decoded, err = url.PathUnescape(strings.TrimPrefix(uri, "data:application/json,"))
		raw = []byte(decoded)
	default:
		target := uri
		if strings.HasPrefix(uri, "ipfs://") {
			path := strings.TrimPrefix(strings.TrimPrefix(uri, "ipfs://"), "ipfs/")
			if path == "" || strings.ContainsAny(path, "?#\\") || strings.Contains(path, "..") {
				out.Status = "blocked"
				return out
			}
			target = "https://ipfs.io/ipfs/" + path
		}
		u, e := url.Parse(target)
		if e != nil || validURL(u) != nil {
			out.Status = "blocked"
			return out
		}
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if e != nil {
			return out
		}
		req.Header.Set("Accept", "application/json")
		res, e := r.client.Do(req)
		if e != nil {
			if errors.Is(e, errBlocked) {
				out.Status = "blocked"
			}
			return out
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return out
		}
		raw, err = io.ReadAll(io.LimitReader(res.Body, maxBytes+1))
	}
	if err != nil || len(raw) > maxBytes || !utf8.Valid(raw) {
		return out
	}
	var fields struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Image       string `json:"image"`
	}
	trimmed := strings.TrimSpace(string(raw))
	if len(trimmed) == 0 || trimmed[0] != '{' || json.Unmarshal(raw, &fields) != nil {
		return out
	}
	if len(fields.Name) > 1024 || len(fields.Description) > 16384 || len(fields.Image) > 4096 {
		return out
	}
	out.Status, out.Name, out.Description = "available", fields.Name, fields.Description
	// Only informational HTTPS/IPFS links; drop javascript, data SVG/HTML, etc.
	if strings.HasPrefix(fields.Image, "ipfs://") {
		out.Image = fields.Image
	} else if u, e := url.Parse(fields.Image); e == nil && validURL(u) == nil {
		out.Image = fields.Image
	}
	return out
}
