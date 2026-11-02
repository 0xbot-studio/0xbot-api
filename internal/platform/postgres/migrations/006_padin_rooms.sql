CREATE TABLE padin_rooms (
 id text PRIMARY KEY,
 workspace_id text NOT NULL REFERENCES workspaces(id),
 state jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX padin_rooms_owner_created ON padin_rooms(workspace_id,created_at);
CREATE TABLE padin_room_receipts (
 workspace_id text NOT NULL REFERENCES workspaces(id),
 idempotency_key text NOT NULL,
 request_hash text NOT NULL,
 response jsonb NOT NULL,
 PRIMARY KEY(workspace_id,idempotency_key)
);
INSERT INTO provider_settings(id,enabled,priority) VALUES('padin-rooms',true,100) ON CONFLICT(id) DO NOTHING;
ALTER TABLE mcp_oauth_grants DROP CONSTRAINT mcp_oauth_grants_scope_check;
ALTER TABLE mcp_oauth_grants ADD CONSTRAINT mcp_oauth_grants_scope_check CHECK(scope ~ '^data:read( messages:read)?( messages:send)?( rooms:read)?( rooms:write)?$');
ALTER TABLE mcp_oauth_grants DROP CONSTRAINT mcp_oauth_grants_wallet_check;
ALTER TABLE mcp_oauth_grants ADD CONSTRAINT mcp_oauth_grants_wallet_check CHECK(scope NOT LIKE '%messages:%' OR wallet ~ '^0x[0-9a-f]{40}$');
