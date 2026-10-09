-- +goose Up
-- Add auth_config to ai_apps for per-app authentication overrides
ALTER TABLE ai_apps ADD COLUMN IF NOT EXISTS auth_config JSONB DEFAULT NULL;

-- +goose Down
ALTER TABLE ai_apps DROP COLUMN IF EXISTS auth_config;
