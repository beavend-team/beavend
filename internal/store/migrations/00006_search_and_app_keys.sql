-- +goose Up
-- Add search_config to ai_app_versions
ALTER TABLE ai_app_versions ADD COLUMN IF NOT EXISTS search_config JSONB NOT NULL DEFAULT '{}'::jsonb;

-- App API Keys for direct server-to-server AI App authentication
CREATE TABLE IF NOT EXISTS app_api_keys (
    id VARCHAR(64) PRIMARY KEY,
    app_id VARCHAR(64) NOT NULL REFERENCES ai_apps(id) ON DELETE CASCADE,
    project_id VARCHAR(64) NOT NULL,
    environment VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    key_prefix VARCHAR(16) NOT NULL,
    key_hash VARCHAR(64) NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    FOREIGN KEY (project_id, environment) REFERENCES environments(project_id, name) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_app_api_keys_lookup ON app_api_keys(key_hash);
CREATE INDEX IF NOT EXISTS idx_app_api_keys_app ON app_api_keys(project_id, environment, app_id);

-- +goose Down
DROP TABLE IF EXISTS app_api_keys;
ALTER TABLE ai_app_versions DROP COLUMN IF EXISTS search_config;
