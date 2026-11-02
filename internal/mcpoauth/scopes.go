package mcpoauth

import (
	"0xbot.org/api/internal/access"
	"0xbot.org/api/internal/platform/token"
	"context"
	"slices"
	"strings"
)

var SupportedScopes = []string{Scope, "messages:read", "messages:send", "rooms:read", "rooms:write"}

func normalizeScope(raw string) (string, error) {
	if raw == "" {
		return Scope, nil
	}
	seen := map[string]bool{}
	for _, s := range strings.Fields(raw) {
		if !slices.Contains(SupportedScopes, s) {
			return "", ErrRequest
		}
		seen[s] = true
	}
	// All current MCP grants include public read capabilities. No silent privilege addition.
	if !seen[Scope] {
		return "", ErrRequest
	}
	out := []string{Scope}
	for _, s := range SupportedScopes[1:] {
		if seen[s] {
			out = append(out, s)
		}
	}
	return strings.Join(out, " "), nil
}
func (s Store) AuthenticateAccess(ctx context.Context, raw string) (access.Principal, error) {
	var p access.Principal
	e := s.DB.QueryRow(ctx, "SELECT g.workspace_id,g.scope,g.wallet FROM mcp_oauth_tokens t JOIN mcp_oauth_grants g ON g.id=t.grant_id WHERE t.hash=$1 AND t.kind='access' AND t.expires_at>now() AND g.expires_at>now() AND g.revoked_at IS NULL AND g.resource=$2", token.Hash(raw), s.Resource).Scan(&p.Workspace, &p.Scope, &p.Wallet)
	if e != nil {
		return access.Principal{}, ErrGrant
	}
	return p, nil
}
