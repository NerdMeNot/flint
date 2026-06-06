-- +goose Up

-- Per-org limit on concurrent running steps (K8s Jobs).
-- Default 20 = reasonable for a small-to-medium cluster.
ALTER TABLE orgs ADD COLUMN IF NOT EXISTS concurrency_limit int NOT NULL DEFAULT 20;

-- +goose Down

ALTER TABLE orgs DROP COLUMN IF EXISTS concurrency_limit;
