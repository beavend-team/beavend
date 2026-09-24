-- name: GetRateLimitCounter :one
SELECT current_count
FROM rate_limit_counters
WHERE key = $1 AND resource = $2 AND window_seconds = $3 AND bucket_start = $4;

-- name: UpsertRateLimitCounter :one
INSERT INTO rate_limit_counters (key, resource, window_seconds, bucket_start, current_count, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (key, resource, window_seconds, bucket_start)
DO UPDATE SET current_count = rate_limit_counters.current_count + EXCLUDED.current_count
RETURNING current_count;

-- name: AdjustRateLimitCounter :exec
UPDATE rate_limit_counters
SET current_count = GREATEST(0, current_count + $5)
WHERE key = $1 AND resource = $2 AND window_seconds = $3 AND bucket_start = $4;

-- name: DeleteExpiredRateLimits :exec
DELETE FROM rate_limit_counters
WHERE expires_at < $1;
