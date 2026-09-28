-- name: InsertRequestLog :exec
INSERT INTO request_logs (
    id,
    request_id,
    project_id,
    environment,
    ai_app_id,
    user_id,
    user_subject,
    user_issuer,
    logical_model,
    provider,
    model_deployment,
    status,
    status_code,
    latency_ms,
    input_tokens,
    output_tokens,
    total_tokens,
    cost_usd,
    is_streaming,
    tool_calls,
    error_code,
    error_message,
    metadata,
    created_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24
);

-- name: InsertUsageEvent :exec
INSERT INTO usage_events (
    id,
    request_id,
    project_id,
    environment,
    ai_app_id,
    user_id,
    logical_model,
    provider,
    input_tokens,
    output_tokens,
    total_tokens,
    cost_usd,
    created_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
);

-- name: UpsertUsageRollup :exec
INSERT INTO usage_rollups (
    project_id,
    environment,
    ai_app_id,
    user_id,
    logical_model,
    provider,
    granularity,
    bucket_start,
    request_count,
    input_tokens,
    output_tokens,
    total_tokens,
    cost_usd,
    error_count,
    updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15
)
ON CONFLICT (project_id, environment, ai_app_id, user_id, logical_model, provider, granularity, bucket_start)
DO UPDATE SET
    request_count = EXCLUDED.request_count,
    input_tokens = EXCLUDED.input_tokens,
    output_tokens = EXCLUDED.output_tokens,
    total_tokens = EXCLUDED.total_tokens,
    cost_usd = EXCLUDED.cost_usd,
    error_count = EXCLUDED.error_count,
    updated_at = NOW();

-- name: GetUsageRollups :many
SELECT
    project_id,
    environment,
    ai_app_id,
    user_id,
    logical_model,
    provider,
    granularity,
    bucket_start,
    request_count,
    input_tokens,
    output_tokens,
    total_tokens,
    cost_usd,
    error_count,
    updated_at
FROM usage_rollups
WHERE project_id = $1
  AND environment = $2
  AND granularity = $3
  AND bucket_start >= $4
  AND bucket_start <= $5
ORDER BY bucket_start ASC;

-- name: GetRequestLogs :many
SELECT
    id,
    request_id,
    project_id,
    environment,
    ai_app_id,
    user_id,
    user_subject,
    user_issuer,
    logical_model,
    provider,
    model_deployment,
    status,
    status_code,
    latency_ms,
    input_tokens,
    output_tokens,
    total_tokens,
    cost_usd,
    is_streaming,
    tool_calls,
    error_code,
    error_message,
    metadata,
    created_at
FROM request_logs
WHERE project_id = $1
  AND environment = $2
  AND created_at >= $3
  AND created_at <= $4
ORDER BY created_at DESC
LIMIT $5 OFFSET $6;

-- name: GetRequestLogByID :one
SELECT
    id,
    request_id,
    project_id,
    environment,
    ai_app_id,
    user_id,
    user_subject,
    user_issuer,
    logical_model,
    provider,
    model_deployment,
    status,
    status_code,
    latency_ms,
    input_tokens,
    output_tokens,
    total_tokens,
    cost_usd,
    is_streaming,
    tool_calls,
    error_code,
    error_message,
    metadata,
    created_at
FROM request_logs
WHERE request_id = $1
LIMIT 1;

-- name: AggregateUsageEventsForWindow :many
SELECT
    project_id,
    environment,
    ai_app_id,
    user_id,
    logical_model,
    provider,
    COUNT(*)::BIGINT AS request_count,
    COALESCE(SUM(input_tokens), 0)::BIGINT AS input_tokens,
    COALESCE(SUM(output_tokens), 0)::BIGINT AS output_tokens,
    COALESCE(SUM(total_tokens), 0)::BIGINT AS total_tokens,
    COALESCE(SUM(cost_usd), 0)::NUMERIC(14, 6) AS cost_usd
FROM usage_events
WHERE created_at >= $1 AND created_at < $2
GROUP BY project_id, environment, ai_app_id, user_id, logical_model, provider;

-- name: AggregateRequestErrorsForWindow :many
SELECT
    project_id,
    environment,
    ai_app_id,
    user_id,
    logical_model,
    provider,
    COUNT(*)::BIGINT AS error_count
FROM request_logs
WHERE created_at >= $1 AND created_at < $2
  AND status != 'success'
GROUP BY project_id, environment, ai_app_id, user_id, logical_model, provider;

