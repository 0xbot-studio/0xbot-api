package operations

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ DB *pgxpool.Pool }

func (s Store) Record(ctx context.Context, r Record) error {
	_, e := s.DB.Exec(ctx, "INSERT INTO operations(id,workspace_id,capability,provider,status,latency_ms,error_code,fee_usd,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(id) DO NOTHING", r.ID, r.WorkspaceID, r.Capability, r.Provider, r.Status, r.LatencyMS, r.ErrorCode, r.FeeUSD, r.CreatedAt)
	return e
}
func (s Store) List(ctx context.Context, workspace string, limit, offset int) ([]Record, error) {
	rows, e := s.DB.Query(ctx, "SELECT id,capability,provider,status,latency_ms,error_code,fee_usd,created_at FROM operations WHERE workspace_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3", workspace, limit, offset)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Record{}
	for rows.Next() {
		var r Record
		if e = rows.Scan(&r.ID, &r.Capability, &r.Provider, &r.Status, &r.LatencyMS, &r.ErrorCode, &r.FeeUSD, &r.CreatedAt); e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type Usage struct {
	Requests  int64  `json:"requests"`
	Succeeded int64  `json:"succeeded"`
	Failed    int64  `json:"failed"`
	FeeUSD    string `json:"fee_usd"`
}

func (s Store) Usage(ctx context.Context, w string) (Usage, error) {
	var u Usage
	e := s.DB.QueryRow(ctx, "SELECT count(*),count(*) FILTER(WHERE status='succeeded'),count(*) FILTER(WHERE status='failed'),COALESCE(sum(fee_usd::numeric),0)::text FROM operations WHERE workspace_id=$1 AND created_at>=date_trunc('month',now() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'", w).Scan(&u.Requests, &u.Succeeded, &u.Failed, &u.FeeUSD)
	return u, e
}
