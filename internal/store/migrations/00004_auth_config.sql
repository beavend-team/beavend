-- +goose Up
ALTER TABLE environments ADD COLUMN IF NOT EXISTS auth_config JSONB NOT NULL DEFAULT '{}'::jsonb;

-- +goose Down
ALTER TABLE environments DROP COLUMN IF EXISTS auth_config;
