-- +goose Up

-- Partitioned table for request metadata logs
CREATE TABLE IF NOT EXISTS request_logs (
    id VARCHAR(64) NOT NULL,
    request_id VARCHAR(64) NOT NULL,
    project_id VARCHAR(64) NOT NULL,
    environment VARCHAR(64) NOT NULL,
    ai_app_id VARCHAR(64) NOT NULL,
    user_id VARCHAR(255) NOT NULL DEFAULT '',
    user_subject VARCHAR(255) NOT NULL DEFAULT '',
    user_issuer VARCHAR(255) NOT NULL DEFAULT '',
    logical_model VARCHAR(128) NOT NULL DEFAULT '',
    provider VARCHAR(64) NOT NULL DEFAULT '',
    model_deployment VARCHAR(255) NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'success',
    status_code INT NOT NULL DEFAULT 200,
    latency_ms BIGINT NOT NULL DEFAULT 0,
    input_tokens INT NOT NULL DEFAULT 0,
    output_tokens INT NOT NULL DEFAULT 0,
    total_tokens INT NOT NULL DEFAULT 0,
    cost_usd NUMERIC(12, 6) NOT NULL DEFAULT 0.0,
    is_streaming BOOLEAN NOT NULL DEFAULT FALSE,
    tool_calls JSONB NOT NULL DEFAULT '[]'::jsonb,
    error_code VARCHAR(64) NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

CREATE TABLE IF NOT EXISTS request_logs_default PARTITION OF request_logs DEFAULT;

CREATE INDEX IF NOT EXISTS idx_request_logs_project_env_created ON request_logs (project_id, environment, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_request_logs_request_id ON request_logs (request_id);
CREATE INDEX IF NOT EXISTS idx_request_logs_app_created ON request_logs (ai_app_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_request_logs_user_created ON request_logs (user_id, created_at DESC);

-- Partitioned table for usage events
CREATE TABLE IF NOT EXISTS usage_events (
    id VARCHAR(64) NOT NULL,
    request_id VARCHAR(64) NOT NULL,
    project_id VARCHAR(64) NOT NULL,
    environment VARCHAR(64) NOT NULL,
    ai_app_id VARCHAR(64) NOT NULL,
    user_id VARCHAR(255) NOT NULL DEFAULT '',
    logical_model VARCHAR(128) NOT NULL DEFAULT '',
    provider VARCHAR(64) NOT NULL DEFAULT '',
    input_tokens INT NOT NULL DEFAULT 0,
    output_tokens INT NOT NULL DEFAULT 0,
    total_tokens INT NOT NULL DEFAULT 0,
    cost_usd NUMERIC(12, 6) NOT NULL DEFAULT 0.0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

CREATE TABLE IF NOT EXISTS usage_events_default PARTITION OF usage_events DEFAULT;

CREATE INDEX IF NOT EXISTS idx_usage_events_project_env_created ON usage_events (project_id, environment, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_usage_events_app_created ON usage_events (ai_app_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_usage_events_rollup ON usage_events (created_at, project_id, environment, ai_app_id);

-- Summary table for aggregated rollups (hourly and daily)
CREATE TABLE IF NOT EXISTS usage_rollups (
    project_id VARCHAR(64) NOT NULL,
    environment VARCHAR(64) NOT NULL,
    ai_app_id VARCHAR(64) NOT NULL,
    user_id VARCHAR(255) NOT NULL DEFAULT '',
    logical_model VARCHAR(128) NOT NULL DEFAULT '',
    provider VARCHAR(64) NOT NULL DEFAULT '',
    granularity VARCHAR(16) NOT NULL, -- 'hour', 'day'
    bucket_start TIMESTAMPTZ NOT NULL,
    request_count BIGINT NOT NULL DEFAULT 0,
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    total_tokens BIGINT NOT NULL DEFAULT 0,
    cost_usd NUMERIC(14, 6) NOT NULL DEFAULT 0.0,
    error_count BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (project_id, environment, ai_app_id, user_id, logical_model, provider, granularity, bucket_start)
);

CREATE INDEX IF NOT EXISTS idx_usage_rollups_query ON usage_rollups (project_id, environment, granularity, bucket_start DESC);
CREATE INDEX IF NOT EXISTS idx_usage_rollups_app ON usage_rollups (ai_app_id, granularity, bucket_start DESC);

-- +goose Down
DROP TABLE IF EXISTS usage_rollups;
DROP TABLE IF EXISTS usage_events;
DROP TABLE IF EXISTS request_logs;
