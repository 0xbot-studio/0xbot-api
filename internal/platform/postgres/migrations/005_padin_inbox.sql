CREATE TABLE padin_messages (
 id text PRIMARY KEY,
 workspace_id text NOT NULL REFERENCES workspaces(id),
 sender text NOT NULL,
 recipient text NOT NULL,
 body text NOT NULL CHECK (octet_length(body) BETWEEN 1 AND 4096),
 idempotency_key text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(workspace_id,idempotency_key)
);
CREATE INDEX padin_messages_inbox ON padin_messages(recipient,created_at DESC,id DESC);
CREATE INDEX padin_messages_sender_time ON padin_messages(sender,created_at DESC);
INSERT INTO provider_settings(id,enabled,priority) VALUES('padin-inbox',true,100) ON CONFLICT(id) DO NOTHING;
ALTER TABLE mcp_oauth_requests ADD COLUMN scope text NOT NULL DEFAULT 'data:read';
ALTER TABLE mcp_oauth_grants DROP CONSTRAINT mcp_oauth_grants_scope_check;
ALTER TABLE mcp_oauth_grants ADD CONSTRAINT mcp_oauth_grants_scope_check CHECK(scope IN ('data:read','data:read messages:read','data:read messages:send','data:read messages:read messages:send'));
ALTER TABLE mcp_oauth_grants ADD COLUMN wallet text NOT NULL DEFAULT '';
ALTER TABLE mcp_oauth_grants ADD CONSTRAINT mcp_oauth_grants_wallet_check CHECK(scope='data:read' OR wallet ~ '^0x[0-9a-f]{40}$');
