package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

type Config struct {
	Addr, DatabaseURL, AppOrigin, AdminOrigin, APIOrigin   string
	Secure                                                 bool
	TrustProxy                                             bool
	Publicnode, DRPC, XClientID, XClientSecret, TOTPSecret string
	AdminWallets                                           map[string]bool
	LIFIKey, LIFIIntegrator                                string
	LIFIPublicAccess                                       bool
}

func env(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}
func Load() (Config, error) {
	c := Config{Addr: env("HTTP_ADDR", "127.0.0.1:8095"), DatabaseURL: os.Getenv("DATABASE_URL"), AppOrigin: env("APP_ORIGIN", "http://localhost:5173"), AdminOrigin: env("ADMIN_ORIGIN", "http://localhost:5174"), APIOrigin: env("API_ORIGIN", "http://localhost:8095"), Secure: env("COOKIE_SECURE", "true") == "true", Publicnode: env("PUBLICNODE_RPC", "https://ethereum-rpc.publicnode.com"), DRPC: env("DRPC_RPC", "https://eth.drpc.org"), XClientID: os.Getenv("X_CLIENT_ID"), XClientSecret: os.Getenv("X_CLIENT_SECRET"), TOTPSecret: os.Getenv("ADMIN_TOTP_SECRET"), AdminWallets: map[string]bool{}}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("DATABASE_URL is required")
	}
	c.LIFIKey = os.Getenv("LIFI_API_KEY")
	c.LIFIPublicAccess = os.Getenv("LIFI_PUBLIC_ACCESS") == "true"
	c.LIFIIntegrator = env("LIFI_INTEGRATOR", "0xbot")
	if len(c.LIFIIntegrator) > 64 || strings.ContainsAny(c.LIFIIntegrator, " \r\n\t") {
		return c, fmt.Errorf("invalid LIFI_INTEGRATOR")
	}
	c.TrustProxy = os.Getenv("TRUST_PROXY") == "true"
	for _, origin := range []string{c.AppOrigin, c.AdminOrigin, c.APIOrigin} {
		u, e := url.Parse(origin)
		if e != nil || u.Host == "" || u.Path != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
			return c, fmt.Errorf("invalid configured origin")
		}
		if c.Secure && u.Scheme != "https" {
			return c, fmt.Errorf("secure cookies require HTTPS origins")
		}
	}
	for _, a := range strings.Split(os.Getenv("ADMIN_WALLETS"), ",") {
		a = strings.ToLower(strings.TrimSpace(a))
		if a != "" {
			c.AdminWallets[a] = true
		}
	}
	return c, nil
}
