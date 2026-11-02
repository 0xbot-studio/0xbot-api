CREATE TABLE users (
 id text PRIMARY KEY,
 display_name text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE identities (
 kind text NOT NULL CHECK(kind IN ('wallet','x')),
 subject text NOT NULL,
 user_id text NOT NULL REFERENCES users(id),
 PRIMARY KEY(kind,subject)
);
CREATE TABLE workspaces (
 id text PRIMARY KEY,
 owner_id text NOT NULL UNIQUE REFERENCES users(id),
 name text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE auth_challenges (
 id text PRIMARY KEY,
 address text NOT NULL,
 message text NOT NULL,
 origin text NOT NULL,
 binding_user text REFERENCES users(id),
 expires_at timestamptz NOT NULL
);
CREATE TABLE sessions (
 token_hash text PRIMARY KEY,
 user_id text NOT NULL REFERENCES users(id),
 audience text NOT NULL,
 admin_verified_at timestamptz,
 expires_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sessions_expiry ON sessions(expires_at);
CREATE TABLE oauth_states (
 state_hash text PRIMARY KEY,
 browser_hash text NOT NULL,
 verifier text NOT NULL,
 origin text NOT NULL,
 binding_user text REFERENCES users(id),
 expires_at timestamptz NOT NULL
);
CREATE TABLE api_keys (
 id text PRIMARY KEY,
 workspace_id text NOT NULL REFERENCES workspaces(id),
 name text NOT NULL,
 prefix text NOT NULL,
 token_hash text NOT NULL UNIQUE,
 scope text NOT NULL DEFAULT 'data:read',
 created_at timestamptz NOT NULL DEFAULT now(),
 revoked_at timestamptz
);
CREATE INDEX api_keys_workspace ON api_keys(workspace_id,created_at DESC);
CREATE TABLE provider_settings (
 id text PRIMARY KEY,
 enabled boolean NOT NULL DEFAULT true,
 priority integer NOT NULL CHECK(priority BETWEEN 1 AND 100),
 updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO provider_settings(id,priority) VALUES ('publicnode',10),('drpc',20);
CREATE TABLE operations (
 id text PRIMARY KEY,
 workspace_id text NOT NULL REFERENCES workspaces(id),
 capability text NOT NULL,
 provider text NOT NULL,
 status text NOT NULL CHECK(status IN ('succeeded','failed')),
 latency_ms bigint NOT NULL,
 error_code text NOT NULL DEFAULT '',
 fee_usd text NOT NULL DEFAULT '0',
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX operations_workspace ON operations(workspace_id,created_at DESC);
CREATE TABLE audit_log (
 id text PRIMARY KEY,
 user_id text NOT NULL REFERENCES users(id),
 action text NOT NULL,
 target text NOT NULL,
 details jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE rate_windows (
 key text PRIMARY KEY,
 count integer NOT NULL,
 expires_at timestamptz NOT NULL
);

