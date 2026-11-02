package mcpoauth

import (
	"0xbot.org/api/internal/identity"
	"0xbot.org/api/internal/messaging"
	"0xbot.org/api/internal/platform/token"
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/url"
	"strings"
	"time"
)

type Store struct {
	DB       *pgxpool.Pool
	Resource string
	Issuer   string
}

func (s Store) Register(ctx context.Context, in Registration) (Client, error) {
	refresh, e := validateRegistration(&in)
	if e != nil {
		return Client{}, e
	}
	c := Client{ID: token.New(), Name: in.Name, Redirects: in.Redirects, AuthMethod: "none", GrantTypes: in.GrantTypes, ResponseTypes: in.ResponseTypes}
	raw, _ := json.Marshal(in.Redirects)
	_, e = s.DB.Exec(ctx, "INSERT INTO mcp_oauth_clients(id,name,redirects,refresh_allowed) VALUES($1,$2,$3,$4)", c.ID, c.Name, raw, refresh)
	return c, e
}
func (s Store) Begin(ctx context.Context, a Authorization, browser string) (string, error) {
	scope, scopeErr := normalizeScope(a.Scope)
	if scopeErr != nil {
		return "", scopeErr
	}
	if a.ResponseType != "code" || a.Method != "S256" || !challengePattern.MatchString(a.Challenge) || a.Resource != s.Resource || len(a.State) > 1024 {
		return "", ErrRequest
	}
	var raw []byte
	if e := s.DB.QueryRow(ctx, "SELECT redirects FROM mcp_oauth_clients WHERE id=$1", a.ClientID).Scan(&raw); e != nil {
		return "", ErrClient
	}
	var redirects []string
	if e := json.Unmarshal(raw, &redirects); e != nil {
		return "", e
	}
	found := false
	for _, v := range redirects {
		if redirectMatches(v, a.RedirectURI) {
			found = true
		}
	}
	if !found {
		return "", ErrRequest
	}
	id := token.New()
	_, e := s.DB.Exec(ctx, "INSERT INTO mcp_oauth_requests(id,browser_hash,client_id,redirect_uri,state,challenge,resource,scope) VALUES($1,$2,$3,$4,$5,$6,$7,$8)", id, token.Hash(browser), a.ClientID, a.RedirectURI, a.State, a.Challenge, a.Resource, scope)
	return id, e
}
func (s Store) Consent(ctx context.Context, id, browser string) (Consent, error) {
	c := Consent{Scope: Scope}
	e := s.DB.QueryRow(ctx, "SELECT r.id,c.id,c.name,r.redirect_uri,r.resource,r.scope FROM mcp_oauth_requests r JOIN mcp_oauth_clients c ON c.id=r.client_id WHERE r.id=$1 AND r.browser_hash=$2 AND r.expires_at>now()", id, token.Hash(browser)).Scan(&c.ID, &c.ClientID, &c.ClientName, &c.RedirectURI, &c.Resource, &c.Scope)
	if e != nil {
		return Consent{}, ErrGrant
	}
	return c, nil
}
func (s Store) Decide(ctx context.Context, id, browser, workspace string, approve bool) (string, error) {
	return s.DecideWallet(ctx, id, browser, workspace, approve, "")
}
func (s Store) DecideWallet(ctx context.Context, id, browser, workspace string, approve bool, wallet string) (string, error) {
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return "", e
	}
	defer tx.Rollback(ctx)
	var client, redirect, state, challenge, resource, scope string
	e = tx.QueryRow(ctx, "DELETE FROM mcp_oauth_requests WHERE id=$1 AND browser_hash=$2 AND expires_at>now() RETURNING client_id,redirect_uri,state,challenge,resource,scope", id, token.Hash(browser)).Scan(&client, &redirect, &state, &challenge, &resource, &scope)
	if e != nil {
		return "", ErrGrant
	}
	u, e := url.Parse(redirect)
	if e != nil {
		return "", ErrGrant
	}
	q := u.Query()
	q.Set("state", state)
	q.Set("iss", s.Issuer)
	if approve {
		if strings.Contains(scope, "messages:") {
			var err error
			wallet, err = messaging.Address(wallet)
			if err != nil {
				return "", ErrRequest
			}
			owns, err := (identity.Store{DB: s.DB}).OwnsWallet(ctx, workspace, wallet)
			if err != nil {
				return "", err
			}
			if !owns {
				return "", ErrRequest
			}
		} else {
			wallet = ""
		}
		// Match the bounded connected-app listing; serialize approvals per workspace.
		if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "mcp-consent:"+workspace); e != nil {
			return "", e
		}
		var active int
		if e = tx.QueryRow(ctx, "SELECT count(*) FROM mcp_oauth_grants WHERE workspace_id=$1 AND revoked_at IS NULL AND expires_at>now()", workspace).Scan(&active); e != nil {
			return "", e
		}
		if active >= 100 {
			return "", ErrRequest
		}
		grant, code := token.New(), token.New()
		_, e = tx.Exec(ctx, "INSERT INTO mcp_oauth_grants(id,workspace_id,client_id,scope,resource,wallet) VALUES($1,$2,$3,$4,$5,$6)", grant, workspace, client, scope, resource, wallet)
		if e != nil {
			return "", e
		}
		_, e = tx.Exec(ctx, "INSERT INTO mcp_oauth_codes(hash,grant_id,redirect_uri,challenge) VALUES($1,$2,$3,$4)", token.Hash(code), grant, redirect, challenge)
		if e != nil {
			return "", e
		}
		q.Set("code", code)
	} else {
		q.Set("error", "access_denied")
	}
	u.RawQuery = q.Encode()
	if e = tx.Commit(ctx); e != nil {
		return "", e
	}
	return u.String(), nil
}
func (s Store) Authenticate(ctx context.Context, raw string) (string, error) {
	var workspace string
	e := s.DB.QueryRow(ctx, "SELECT g.workspace_id FROM mcp_oauth_tokens t JOIN mcp_oauth_grants g ON g.id=t.grant_id WHERE t.hash=$1 AND t.kind='access' AND t.expires_at>now() AND g.expires_at>now() AND g.revoked_at IS NULL AND g.resource=$2 AND g.scope=$3", token.Hash(raw), s.Resource, Scope).Scan(&workspace)
	if e != nil {
		return "", ErrGrant
	}
	return workspace, nil
}
func (s Store) List(ctx context.Context, workspace string) ([]Grant, error) {
	rows, e := s.DB.Query(ctx, "SELECT g.id,c.name,g.scope,g.created_at,g.expires_at,g.wallet FROM mcp_oauth_grants g JOIN mcp_oauth_clients c ON c.id=g.client_id WHERE g.workspace_id=$1 AND g.revoked_at IS NULL AND g.expires_at>now() ORDER BY g.created_at DESC LIMIT 100", workspace)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Grant{}
	for rows.Next() {
		var g Grant
		if e = rows.Scan(&g.ID, &g.ClientName, &g.Scope, &g.CreatedAt, &g.ExpiresAt, &g.Wallet); e != nil {
			return nil, e
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
func (s Store) RevokeGrant(ctx context.Context, workspace, id string) error {
	tag, e := s.DB.Exec(ctx, "UPDATE mcp_oauth_grants SET revoked_at=now() WHERE id=$1 AND workspace_id=$2 AND revoked_at IS NULL", id, workspace)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrGrant
	}
	return nil
}
func (s Store) RevokeToken(ctx context.Context, client, raw string) error {
	// RFC 7009: unknown tokens are successful; possessing a token can only revoke its own client grant.
	_, e := s.DB.Exec(ctx, "UPDATE mcp_oauth_grants SET revoked_at=now() WHERE client_id=$1 AND id IN (SELECT grant_id FROM mcp_oauth_tokens WHERE hash=$2)", client, token.Hash(raw))
	return e
}
func (s Store) Cleanup(ctx context.Context) error {
	// Keep spent refresh tokens until grant expiry so replay detection remains effective.
	_, e := s.DB.Exec(ctx, "DELETE FROM mcp_oauth_requests WHERE expires_at<now()")
	if e != nil {
		return e
	}
	_, e = s.DB.Exec(ctx, "DELETE FROM mcp_oauth_grants WHERE expires_at < $1", time.Now().Add(-24*time.Hour))
	if e != nil {
		return e
	}
	_, e = s.DB.Exec(ctx, "DELETE FROM mcp_oauth_clients c WHERE c.created_at < now()-interval '7 days' AND NOT EXISTS(SELECT 1 FROM mcp_oauth_grants g WHERE g.client_id=c.id) AND NOT EXISTS(SELECT 1 FROM mcp_oauth_requests r WHERE r.client_id=c.id)")
	return e
}
