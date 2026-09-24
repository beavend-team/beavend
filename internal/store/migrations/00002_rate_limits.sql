-- +goose Up
CREATE TABLE IF NOT EXISTS rate_limit_counters (
    key VARCHAR(255) NOT NULL,
    resource VARCHAR(32) NOT NULL,
    window_seconds INT NOT NULL,
    bucket_start TIMESTAMPTZ NOT NULL,
    current_count BIGINT NOT NULL DEFAULT 0,
    expires_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (key, resource, window_seconds, bucket_start)
);

CREATE INDEX IF NOT EXISTS idx_rate_limit_counters_expires ON rate_limit_counters(expires_at);

-- +goose Down
DROP TABLE IF EXISTS rate_limit_counters;
