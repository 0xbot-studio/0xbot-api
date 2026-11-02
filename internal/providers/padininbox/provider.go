// Package padininbox implements the PADIN Inbox provider. No cross-network delivery.
package padininbox

import (
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/messaging"
	"0xbot.org/api/internal/operations"
	"0xbot.org/api/internal/platform/token"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

type Provider struct{ DB *pgxpool.Pool }

func (p Provider) ID() string                   { return messaging.ProviderID }
func (p Provider) Supports(network string) bool { return network == "eip155:1" }
func (p Provider) CapabilityIDs() []string {
	return []string{"messaging.send", "messaging.list", "messaging.get"}
}
func (p Provider) Send(ctx context.Context, workspace string, in messaging.SendInput) (messaging.Message, error) {
	if e := in.Validate(); e != nil {
		return messaging.Message{}, e
	}
	tx, e := p.DB.Begin(ctx)
	if e != nil {
		return messaging.Message{}, e
	}
	defer tx.Rollback(ctx)
	// Serialize a sender's idempotency/quota checks; repeat payloads never consume send quota.
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "padin-send:"+workspace); e != nil {
		return messaging.Message{}, e
	}
	var m messaging.Message
	e = tx.QueryRow(ctx, "SELECT id,sender,recipient,body,created_at FROM padin_messages WHERE workspace_id=$1 AND idempotency_key=$2", workspace, in.IdempotencyKey).Scan(&m.ID, &m.From, &m.To, &m.Body, &m.CreatedAt)
	if e == nil {
		if m.From != in.From || m.To != in.To || m.Body != in.Body {
			return messaging.Message{}, messaging.ErrConflict
		}
		m.Status = "stored"
		return m, tx.Commit(ctx)
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return messaging.Message{}, e
	}
	var minute, day int
	e = tx.QueryRow(ctx, "SELECT count(*) FILTER(WHERE created_at>now()-interval '1 minute'),count(*) FROM padin_messages WHERE workspace_id=$1 AND created_at>now()-interval '1 day'", workspace).Scan(&minute, &day)
	if e != nil {
		return messaging.Message{}, e
	}
	if minute >= 10 || day >= 100 {
		return messaging.Message{}, data.ErrRateLimited
	}
	m = messaging.Message{ID: token.New(), From: in.From, To: in.To, Body: in.Body, Status: "stored"}
	e = tx.QueryRow(ctx, "INSERT INTO padin_messages(id,workspace_id,sender,recipient,body,idempotency_key) VALUES($1,$2,$3,$4,$5,$6) RETURNING created_at", m.ID, workspace, m.From, m.To, m.Body, in.IdempotencyKey).Scan(&m.CreatedAt)
	if e != nil {
		return messaging.Message{}, e
	}
	e = operations.RecordMessageSend(ctx, tx, operations.Record{ID: m.ID, WorkspaceID: workspace, Capability: "messaging.send", Provider: p.ID(), Status: "succeeded", FeeUSD: "0", CreatedAt: m.CreatedAt})
	if e != nil {
		return messaging.Message{}, e
	}
	return m, tx.Commit(ctx)
}

type cursor struct {
	Address, ID string
	Time        time.Time
}

func (p Provider) List(ctx context.Context, in messaging.ListInput) (messaging.Page, error) {
	if e := in.Validate(); e != nil {
		return messaging.Page{}, e
	}
	c := cursor{Address: in.Address, Time: time.Now().Add(time.Minute), ID: ""}
	if in.Cursor != "" {
		raw, e := base64.RawURLEncoding.DecodeString(in.Cursor)
		if e != nil || json.Unmarshal(raw, &c) != nil || c.Address != in.Address || c.Time.IsZero() || len(c.ID) != 64 {
			return messaging.Page{}, data.ErrInput
		}
	}
	rows, e := p.DB.Query(ctx, "SELECT id,sender,recipient,created_at FROM padin_messages WHERE recipient=$1 AND (created_at,id)<($2,$3) ORDER BY created_at DESC,id DESC LIMIT $4", in.Address, c.Time, c.ID, in.Limit+1)
	if e != nil {
		return messaging.Page{}, e
	}
	defer rows.Close()
	out := messaging.Page{Messages: []messaging.Summary{}}
	for rows.Next() {
		var m messaging.Summary
		if e = rows.Scan(&m.ID, &m.From, &m.To, &m.CreatedAt); e != nil {
			return messaging.Page{}, e
		}
		m.Status = "stored"
		out.Messages = append(out.Messages, m)
	}
	if e = rows.Err(); e != nil {
		return messaging.Page{}, e
	}
	if len(out.Messages) > in.Limit {
		out.Messages = out.Messages[:in.Limit]
		last := out.Messages[len(out.Messages)-1]
		raw, _ := json.Marshal(cursor{Address: in.Address, ID: last.ID, Time: last.CreatedAt})
		out.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	return out, nil
}
func (p Provider) Get(ctx context.Context, in messaging.GetInput) (messaging.Message, error) {
	if e := in.Validate(); e != nil {
		return messaging.Message{}, e
	}
	var m messaging.Message
	e := p.DB.QueryRow(ctx, "SELECT id,sender,recipient,body,created_at FROM padin_messages WHERE id=$1 AND recipient=$2", in.ID, in.Address).Scan(&m.ID, &m.From, &m.To, &m.Body, &m.CreatedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		return messaging.Message{}, messaging.ErrNotFound
	}
	if e != nil {
		return messaging.Message{}, e
	}
	m.Status = "stored"
	return m, nil
}

var _ messaging.Provider = Provider{}
