package limits

const (
	// CheckAndDebitScript is the atomic sliding-window check-and-debit Redis Lua script.
	//
	// KEYS:
	// 1..N: base key for each rule (RuleKey)
	// N+1:  token_windows set key (TokenWindowsKey)
	//
	// ARGV:
	// 1: now (Unix timestamp in seconds)
	// 2: rule_count (N)
	// For each rule i (1..N):
	//   3 + (i-1)*4: window_seconds
	//   4 + (i-1)*4: max_limit
	//   5 + (i-1)*4: cost
	//   6 + (i-1)*4: is_token_rule (1 if tokens, 0 otherwise)
	//
	// Returns:
	// {allowed (0 or 1), retry_after (seconds)}
	CheckAndDebitScript = `
local now = tonumber(ARGV[1])
local rule_count = tonumber(ARGV[2])

local max_retry_after = 0
local allowed = 1

-- First pass: check all rules without modifying state
for i = 1, rule_count do
    local base_key = KEYS[i]
    local window = tonumber(ARGV[3 + (i - 1) * 4])
    local max_limit = tonumber(ARGV[4 + (i - 1) * 4])
    local cost = tonumber(ARGV[5 + (i - 1) * 4])

    if window <= 0 then window = 1 end

    local bucket = math.floor(now / window)
    local curr_key = base_key .. ":" .. bucket
    local prev_key = base_key .. ":" .. (bucket - 1)

    local curr_val = tonumber(redis.call("GET", curr_key) or "0")
    local prev_val = tonumber(redis.call("GET", prev_key) or "0")

    local elapsed = now % window
    local weight = (window - elapsed) / window
    local estimated = math.floor(prev_val * weight) + curr_val

    if (estimated + cost) > max_limit then
        allowed = 0
        local retry = window - elapsed
        if retry <= 0 then retry = 1 end
        if retry > max_retry_after then
            max_retry_after = retry
        end
    end
end

-- If any rule is exceeded, reject without debiting
if allowed == 0 then
    return {0, max_retry_after}
end

-- Second pass: debit all rules
local token_windows_key = KEYS[rule_count + 1]

for i = 1, rule_count do
    local base_key = KEYS[i]
    local window = tonumber(ARGV[3 + (i - 1) * 4])
    local cost = tonumber(ARGV[5 + (i - 1) * 4])
    local is_token_rule = tonumber(ARGV[6 + (i - 1) * 4] or "0")

    if window <= 0 then window = 1 end

    local bucket = math.floor(now / window)
    local curr_key = base_key .. ":" .. bucket

    redis.call("INCRBY", curr_key, cost)
    redis.call("EXPIRE", curr_key, window * 2)

    if is_token_rule == 1 and token_windows_key and token_windows_key ~= "" then
        redis.call("SADD", token_windows_key, tostring(window))
        redis.call("EXPIRE", token_windows_key, window * 2)
    end
end

return {1, 0}
`

	// ReconcileScript reconciles estimated tokens with actual consumed tokens.
	//
	// KEYS:
	// 1: token_windows set key
	// 2: user tokens prefix key ({userKey}:tokens)
	//
	// ARGV:
	// 1: now (Unix timestamp in seconds)
	// 2: delta (actual - estimated; negative = refund, positive = extra debit)
	//
	// Returns:
	// 1 on success
	ReconcileScript = `
local token_windows_key = KEYS[1]
local tokens_prefix = KEYS[2]
local now = tonumber(ARGV[1])
local delta = tonumber(ARGV[2])

if delta == 0 then
    return 1
end

local windows = redis.call("SMEMBERS", token_windows_key)
for _, w_str in ipairs(windows) do
    local window = tonumber(w_str)
    if window and window > 0 then
        local bucket = math.floor(now / window)
        local key = tokens_prefix .. ":" .. window .. ":" .. bucket
        local curr = tonumber(redis.call("GET", key) or "0")
        local new_val = curr + delta
        if new_val < 0 then
            new_val = 0
        end
        redis.call("SET", key, new_val)
        redis.call("EXPIRE", key, window * 2)
    end
end

return 1
`
)
