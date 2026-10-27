-- +goose Up
ALTER TABLE admin_users ADD COLUMN IF NOT EXISTS is_disabled BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE admin_users DROP COLUMN IF EXISTS is_disabled;
