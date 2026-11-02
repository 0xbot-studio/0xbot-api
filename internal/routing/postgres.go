package routing

import (
	"0xbot.org/api/internal/platform/token"
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ DB *pgxpool.Pool }

func (s Store) List(ctx context.Context) ([]Setting, error) {
	rows, e := s.DB.Query(ctx, "SELECT id,enabled,priority FROM provider_settings ORDER BY priority,id")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Setting{}
	for rows.Next() {
		var v Setting
		if e = rows.Scan(&v.ID, &v.Enabled, &v.Priority); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s Store) Update(ctx context.Context, actor string, v Setting) error {
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	tag, e := tx.Exec(ctx, "UPDATE provider_settings SET enabled=$2,priority=$3,updated_at=now() WHERE id=$1", v.ID, v.Enabled, v.Priority)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return dataError{}
	}
	b, _ := json.Marshal(v)
	_, e = tx.Exec(ctx, "INSERT INTO audit_log(id,user_id,action,target,details) VALUES($1,$2,'provider.update',$3,$4)", token.New(), actor, v.ID, b)
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}

type dataError struct{}

func (dataError) Error() string { return "unknown provider" }

func (s Store) Audit(ctx context.Context, limit, offset int) ([]map[string]any, error) {
	rows, e := s.DB.Query(ctx, "SELECT id,action,target,details,created_at::text FROM audit_log ORDER BY created_at DESC,id DESC LIMIT $1 OFFSET $2", limit, offset)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, action, target, created string
		var details json.RawMessage
		if e = rows.Scan(&id, &action, &target, &details, &created); e != nil {
			return nil, e
		}
		out = append(out, map[string]any{"id": id, "action": action, "target": target, "details": details, "created_at": created})
	}
	return out, rows.Err()
}
