-- +goose Up

-- Force password change on first login (for bootstrap accounts).
ALTER TABLE users ADD COLUMN IF NOT EXISTS force_password_change boolean NOT NULL DEFAULT false;

-- +goose Down

ALTER TABLE users DROP COLUMN IF EXISTS force_password_change;
