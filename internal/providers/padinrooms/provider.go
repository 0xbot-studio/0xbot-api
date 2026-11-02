// Package padinrooms implements the first public roundtable provider.
package padinrooms

import (
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/operations"
	"0xbot.org/api/internal/platform/token"
	"0xbot.org/api/internal/rooms"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"strconv"
	"time"
)

type Provider struct{ DB *pgxpool.Pool }

func (p Provider) ID() string             { return rooms.ProviderID }
func (p Provider) Supports(n string) bool { return n == "discussion" }
func (p Provider) CapabilityIDs() []string {
	return []string{"rooms.create", "rooms.list", "rooms.get", "rooms.join", "rooms.moderate", "rooms.post", "rooms.question", "rooms.events", "rooms.wait", "rooms.summary"}
}
func (p Provider) Get(ctx context.Context, id string) (rooms.Room, error) {
	var raw []byte
	var r rooms.Room
	e := p.DB.QueryRow(ctx, "SELECT state FROM padin_rooms WHERE id=$1", id).Scan(&raw)
	if errors.Is(e, pgx.ErrNoRows) {
		return r, rooms.ErrNotFound
	}
	if e != nil {
		return r, e
	}
	e = json.Unmarshal(raw, &r)
	return r, e
}
func (p Provider) List(ctx context.Context, before string, limit int) (rooms.Listing, error) {
	out := rooms.Listing{Rooms: []rooms.Room{}}
	rows, e := p.DB.Query(ctx, "SELECT state FROM padin_rooms WHERE ($1='' OR id<$1) ORDER BY id DESC LIMIT $2", before, limit+1)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var r rooms.Room
		if e = rows.Scan(&raw); e != nil {
			return out, e
		}
		if e = json.Unmarshal(raw, &r); e != nil {
			return out, e
		}
		r.Events = []rooms.Event{}
		r.Summary = rooms.Summary{Kind: r.Summary.Kind, MessageSeqs: []int64{}}
		out.Rooms = append(out.Rooms, r)
	}
	if len(out.Rooms) > limit {
		out.Rooms = out.Rooms[:limit]
		out.NextCursor = out.Rooms[limit-1].ID
	}
	return out, rows.Err()
}
func (p Provider) Mutate(ctx context.Context, workspace, actor string, c rooms.Command) (rooms.Result, error) {
	var out rooms.Result
	raw, e := json.Marshal(c)
	if e != nil {
		return out, e
	}
	hash := token.Hash(string(raw))
	tx, e := p.DB.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "rooms:"+workspace); e != nil {
		return out, e
	}
	var previous string
	var response []byte
	e = tx.QueryRow(ctx, "SELECT request_hash,response FROM padin_room_receipts WHERE workspace_id=$1 AND idempotency_key=$2", workspace, c.IdempotencyKey).Scan(&previous, &response)
	if e == nil {
		if previous != hash {
			return out, rooms.ErrConflict
		}
		if e = json.Unmarshal(response, &out); e != nil {
			return out, e
		}
		return out, tx.Commit(ctx)
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return out, e
	}
	var r rooms.Room
	now := time.Now().UTC()
	if c.Action == "create" {
		var count int
		e = tx.QueryRow(ctx, "SELECT count(*) FROM padin_rooms WHERE workspace_id=$1 AND created_at>now()-interval '1 day'", workspace).Scan(&count)
		if e != nil {
			return out, e
		}
		if count >= 10 {
			return out, data.ErrRateLimited
		}
		r = rooms.NewRoom(token.New(), actor, c, now)
	} else {
		e = tx.QueryRow(ctx, "SELECT state FROM padin_rooms WHERE id=$1 FOR UPDATE", c.RoomID).Scan(&raw)
		if errors.Is(e, pgx.ErrNoRows) {
			return out, rooms.ErrNotFound
		}
		if e != nil {
			return out, e
		}
		if e = json.Unmarshal(raw, &r); e != nil {
			return out, e
		}
		if e = r.Apply(actor, c, now); e != nil {
			return out, e
		}
	}
	raw, e = json.Marshal(r)
	if e != nil {
		return out, e
	}
	if c.Action == "create" {
		_, e = tx.Exec(ctx, "INSERT INTO padin_rooms(id,workspace_id,state) VALUES($1,$2,$3)", r.ID, workspace, raw)
	} else {
		_, e = tx.Exec(ctx, "UPDATE padin_rooms SET state=$2 WHERE id=$1", r.ID, raw)
	}
	if e != nil {
		return out, e
	}
	operation := token.New()
	if c.Action == "create" && c.Plan != nil {
		plan := *c.Plan
		plan.Guests = append([]rooms.GuestPlan{}, c.Plan.Guests...)
		for i := range plan.Guests {
			plan.Guests[i].AgentID = token.Hash("padin-rooms:agent:" + r.ID + ":" + strconv.Itoa(i))
		}
		planJSON, err := json.Marshal(plan)
		if err != nil {
			return out, err
		}
		if _, e = tx.Exec(ctx, "INSERT INTO padin_room_plans(room_id,plan,created_by) VALUES($1,$2,$3)", r.ID, planJSON, c.AdminUser); e != nil {
			return out, e
		}
	}
	capability := "rooms." + c.Action
	if c.Action == "promote" || c.Action == "demote" || c.Action == "start" || c.Action == "skip" || c.Action == "close" {
		capability = "rooms.moderate"
	}
	if e = operations.RecordMessageSend(ctx, tx, operations.Record{ID: operation, WorkspaceID: workspace, Capability: capability, Provider: p.ID(), Status: "succeeded", FeeUSD: "0", CreatedAt: now}); e != nil {
		return out, e
	}
	if c.AdminUser != "" {
		// Do not put private prompts or public message bodies in the audit log.
		details, err := json.Marshal(map[string]any{"operation_id": operation, "seq": r.Seq, "action": c.Action})
		if err != nil {
			return out, err
		}
		if _, e = tx.Exec(ctx, "INSERT INTO audit_log(id,user_id,action,target,details) VALUES($1,$2,$3,$4,$5)", token.New(), c.AdminUser, capability, r.ID, details); e != nil {
			return out, e
		}
	}
	out = rooms.Result{ID: operation, Provider: p.ID(), Status: "succeeded", FeeUSD: "0", Data: rooms.Snapshot{Room: r, SelfID: actor}}
	response, e = json.Marshal(out)
	if e != nil {
		return out, e
	}
	_, e = tx.Exec(ctx, "INSERT INTO padin_room_receipts(workspace_id,idempotency_key,request_hash,response) VALUES($1,$2,$3,$4)", workspace, c.IdempotencyKey, hash, response)
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
