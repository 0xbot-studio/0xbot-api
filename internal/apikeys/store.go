package apikeys

import (
	"0xbot.org/api/internal/platform/token"
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

var ErrAuth = errors.New("invalid_api_key")

type Key struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Prefix    string     `json:"prefix"`
	Scope     string     `json:"scope"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at"`
}
type Created struct {
	Key    Key    `json:"key"`
	Secret string `json:"secret"`
}
type Store struct{ DB *pgxpool.Pool }

func (s Store) Create(ctx context.Context, w, name string) (Created, error) {
	raw := "oxb_" + token.New()
	k := Key{ID: token.New(), Name: name, Prefix: raw[:12], Scope: "data:read"}
	e := s.DB.QueryRow(ctx, "INSERT INTO api_keys(id,workspace_id,name,prefix,token_hash) VALUES($1,$2,$3,$4,$5) RETURNING created_at", k.ID, w, k.Name, k.Prefix, token.Hash(raw)).Scan(&k.CreatedAt)
	return Created{k, raw}, e
}
func (s Store) List(ctx context.Context, w string) ([]Key, error) {
	rows, e := s.DB.Query(ctx, "SELECT id,name,prefix,scope,created_at,revoked_at FROM api_keys WHERE workspace_id=$1 ORDER BY created_at DESC LIMIT 100", w)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Key{}
	for rows.Next() {
		var k Key
		if e = rows.Scan(&k.ID, &k.Name, &k.Prefix, &k.Scope, &k.CreatedAt, &k.RevokedAt); e != nil {
			return nil, e
		}
		out = append(out, k)
	}
	return out, rows.Err()
}
func (s Store) Revoke(ctx context.Context, w, id string) error {
	tag, e := s.DB.Exec(ctx, "UPDATE api_keys SET revoked_at=now() WHERE id=$1 AND workspace_id=$2 AND revoked_at IS NULL", id, w)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrAuth
	}
	return nil
}
func (s Store) Authenticate(ctx context.Context, raw string) (string, error) {
	var w string
	e := s.DB.QueryRow(ctx, "SELECT workspace_id FROM api_keys WHERE token_hash=$1 AND revoked_at IS NULL AND scope='data:read'", token.Hash(raw)).Scan(&w)
	if e != nil {
		return "", ErrAuth
	}
	return w, nil
}
