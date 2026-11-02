package operations

import (
	"context"
	"github.com/jackc/pgx/v5"
)

// RecordMessageSend joins the inbox transaction so a retry cannot double-count a send.
// No message body, sender or recipient is recorded in the general operation history.
func RecordMessageSend(ctx context.Context, tx pgx.Tx, r Record) error {
	_, e := tx.Exec(ctx, "INSERT INTO operations(id,workspace_id,capability,provider,status,latency_ms,error_code,fee_usd,created_at) VALUES($1,$2,$3,$4,$5,0,'','0',$6)", r.ID, r.WorkspaceID, r.Capability, r.Provider, r.Status, r.CreatedAt)
	return e
}
