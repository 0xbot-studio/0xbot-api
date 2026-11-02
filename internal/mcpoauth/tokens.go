package mcpoauth

import (
	"0xbot.org/api/internal/platform/token"
	"context"
	"crypto/subtle"
	"github.com/jackc/pgx/v5"
	"time"
)

func (s Store) ExchangeCode(ctx context.Context, in Exchange) (Tokens, error) {
	if !verifierPattern.MatchString(in.Verifier) || in.Resource != s.Resource {
		return Tokens{}, ErrGrant
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return Tokens{}, e
	}
	defer tx.Rollback(ctx)
	var grant, client, redirect, challenge, resource string
	var valid bool
	var used *time.Time
	e = tx.QueryRow(ctx, "SELECT c.grant_id,g.client_id,c.redirect_uri,c.challenge,g.resource,(c.expires_at>now() AND g.expires_at>now() AND g.revoked_at IS NULL),c.used_at FROM mcp_oauth_codes c JOIN mcp_oauth_grants g ON g.id=c.grant_id WHERE c.hash=$1 FOR UPDATE OF g,c", token.Hash(in.Code)).Scan(&grant, &client, &redirect, &challenge, &resource, &valid, &used)
	if e != nil || !valid || client != in.ClientID || redirect != in.RedirectURI || resource != in.Resource || subtle.ConstantTimeCompare([]byte(challenge), []byte(Challenge(in.Verifier))) != 1 {
		return Tokens{}, ErrGrant
	}
	if used != nil {
		if _, e = tx.Exec(ctx, "UPDATE mcp_oauth_grants SET revoked_at=now() WHERE id=$1", grant); e != nil {
			return Tokens{}, e
		}
		if e = tx.Commit(ctx); e != nil {
			return Tokens{}, e
		}
		return Tokens{}, ErrGrant
	}
	if _, e = tx.Exec(ctx, "UPDATE mcp_oauth_codes SET used_at=now() WHERE hash=$1", token.Hash(in.Code)); e != nil {
		return Tokens{}, e
	}
	out, e := s.issue(ctx, tx, grant)
	if e != nil {
		return Tokens{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Tokens{}, e
	}
	return out, nil
}
func (s Store) Refresh(ctx context.Context, in Exchange) (Tokens, error) {
	if in.Resource != s.Resource {
		return Tokens{}, ErrGrant
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return Tokens{}, e
	}
	defer tx.Rollback(ctx)
	var grant, client, resource, scope string
	var valid bool
	var used *time.Time
	e = tx.QueryRow(ctx, "SELECT t.grant_id,g.client_id,g.resource,g.scope,(t.expires_at>now() AND g.expires_at>now() AND g.revoked_at IS NULL),t.used_at FROM mcp_oauth_tokens t JOIN mcp_oauth_grants g ON g.id=t.grant_id WHERE t.hash=$1 AND t.kind='refresh' FOR UPDATE OF g,t", token.Hash(in.RefreshToken)).Scan(&grant, &client, &resource, &scope, &valid, &used)
	if e != nil || !valid || client != in.ClientID || resource != in.Resource {
		return Tokens{}, ErrGrant
	}
	if used != nil {
		if _, e = tx.Exec(ctx, "UPDATE mcp_oauth_grants SET revoked_at=now() WHERE id=$1", grant); e != nil {
			return Tokens{}, e
		}
		if e = tx.Commit(ctx); e != nil {
			return Tokens{}, e
		}
		return Tokens{}, ErrGrant
	}
	if in.Scope != "" {
		requested, err := normalizeScope(in.Scope)
		if err != nil || requested != scope {
			return Tokens{}, ErrGrant
		}
	}
	if _, e = tx.Exec(ctx, "UPDATE mcp_oauth_tokens SET used_at=now() WHERE hash=$1", token.Hash(in.RefreshToken)); e != nil {
		return Tokens{}, e
	}
	out, e := s.issue(ctx, tx, grant)
	if e != nil {
		return Tokens{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Tokens{}, e
	}
	return out, nil
}
func (s Store) issue(ctx context.Context, tx pgx.Tx, grant string) (Tokens, error) {
	var refresh bool
	var scope string
	var expires time.Time
	e := tx.QueryRow(ctx, "SELECT c.refresh_allowed,g.expires_at,g.scope FROM mcp_oauth_grants g JOIN mcp_oauth_clients c ON c.id=g.client_id WHERE g.id=$1", grant).Scan(&refresh, &expires, &scope)
	if e != nil {
		return Tokens{}, e
	}
	out := Tokens{AccessToken: "oxbo_at_" + token.New(), TokenType: "Bearer", ExpiresIn: 900, Scope: scope}
	accessExpires := time.Now().Add(15 * time.Minute)
	if expires.Before(accessExpires) {
		accessExpires = expires
		out.ExpiresIn = int(time.Until(expires).Seconds())
	}
	_, e = tx.Exec(ctx, "INSERT INTO mcp_oauth_tokens(hash,grant_id,kind,expires_at) VALUES($1,$2,'access',$3)", token.Hash(out.AccessToken), grant, accessExpires)
	if e != nil {
		return Tokens{}, e
	}
	if refresh {
		out.RefreshToken = "oxbo_rt_" + token.New()
		_, e = tx.Exec(ctx, "INSERT INTO mcp_oauth_tokens(hash,grant_id,kind,expires_at) VALUES($1,$2,'refresh',$3)", token.Hash(out.RefreshToken), grant, expires)
	}
	return out, e
}
