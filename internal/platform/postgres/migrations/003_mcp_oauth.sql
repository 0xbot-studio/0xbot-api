CREATE TABLE mcp_oauth_clients (
 id text PRIMARY KEY,
 name text NOT NULL,
 redirects jsonb NOT NULL,
 refresh_allowed boolean NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE mcp_oauth_requests (
 id text PRIMARY KEY,
 browser_hash text NOT NULL,
 client_id text NOT NULL REFERENCES mcp_oauth_clients(id),
 redirect_uri text NOT NULL,
 state text NOT NULL,
 challenge text NOT NULL,
 resource text NOT NULL,
 expires_at timestamptz NOT NULL DEFAULT now() + interval '10 minutes'
);
CREATE INDEX mcp_oauth_requests_expiry ON mcp_oauth_requests(expires_at);
CREATE TABLE mcp_oauth_grants (
 id text PRIMARY KEY,
 workspace_id text NOT NULL REFERENCES workspaces(id),
 client_id text NOT NULL REFERENCES mcp_oauth_clients(id),
 scope text NOT NULL CHECK (scope = 'data:read'),
 resource text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL DEFAULT now() + interval '30 days',
 revoked_at timestamptz
);
CREATE INDEX mcp_oauth_grants_workspace ON mcp_oauth_grants(workspace_id, created_at DESC);
CREATE TABLE mcp_oauth_codes (
 hash text PRIMARY KEY,
 grant_id text NOT NULL REFERENCES mcp_oauth_grants(id) ON DELETE CASCADE,
 redirect_uri text NOT NULL,
 challenge text NOT NULL,
 expires_at timestamptz NOT NULL DEFAULT now() + interval '60 seconds',
 used_at timestamptz
);
CREATE TABLE mcp_oauth_tokens (
 hash text PRIMARY KEY,
 grant_id text NOT NULL REFERENCES mcp_oauth_grants(id) ON DELETE CASCADE,
 kind text NOT NULL CHECK (kind IN ('access','refresh')),
 expires_at timestamptz NOT NULL,
 used_at timestamptz
);
CREATE INDEX mcp_oauth_tokens_grant ON mcp_oauth_tokens(grant_id);
CREATE INDEX mcp_oauth_tokens_expiry ON mcp_oauth_tokens(expires_at);
