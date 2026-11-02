package postgres

import (
	"context"
	"embed"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

func Open(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	p, e := pgxpool.New(ctx, dsn)
	if e != nil {
		return nil, e
	}
	if e = p.Ping(ctx); e != nil {
		p.Close()
		return nil, e
	}
	tx, e := p.Begin(ctx)
	if e != nil {
		p.Close()
		return nil, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(950095)"); e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, "CREATE TABLE IF NOT EXISTS schema_versions (version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())"); e != nil {
		return nil, e
	}
	entries, e := migrations.ReadDir("migrations")
	if e != nil {
		return nil, e
	}
	for _, entry := range entries {
		var exists bool
		if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_versions WHERE version=$1)", entry.Name()).Scan(&exists); e != nil {
			return nil, e
		}
		if exists {
			continue
		}
		b, e := migrations.ReadFile("migrations/" + entry.Name())
		if e != nil {
			return nil, e
		}
		if _, e = tx.Exec(ctx, string(b)); e != nil {
			return nil, fmt.Errorf("migration %s: %w", entry.Name(), e)
		}
		if _, e = tx.Exec(ctx, "INSERT INTO schema_versions(version) VALUES($1)", entry.Name()); e != nil {
			return nil, e
		}
	}
	if e = tx.Commit(ctx); e != nil {
		p.Close()
		return nil, e
	}
	return p, nil
}
