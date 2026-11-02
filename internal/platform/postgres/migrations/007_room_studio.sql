-- Private, immutable planning snapshots. Public room state and receipts do not
-- contain prompts, model configuration or budget plans. No inference is triggered.
CREATE TABLE padin_room_plans (
 room_id text PRIMARY KEY REFERENCES padin_rooms(id),
 plan jsonb NOT NULL,
 created_by text NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL DEFAULT now()
);
