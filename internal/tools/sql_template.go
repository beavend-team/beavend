package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib" // registers "pgx" driver

	"github.com/beavend/beavend/internal/core"
)

var (
	// ErrMutatingSQLRejected is returned when a SQL template contains mutating or prohibited statements.
	ErrMutatingSQLRejected = errors.New("sql template contains mutating or prohibited statement")
	// ErrInvalidSQLTemplate is returned when the template syntax or parameters are malformed.
	ErrInvalidSQLTemplate = errors.New("invalid sql template")
	// ErrBlockedSQLConnection is returned when a database connection targets a prohibited host or IP.
	ErrBlockedSQLConnection = errors.New("blocked sql connection target")

	namedParamRegex = regexp.MustCompile(`(^|[^:]):([a-zA-Z_][a-zA-Z0-9_]*)`)
	sqlCommentRegex = regexp.MustCompile(`(?s)(/\*.*?\*/)|(--[^\r\n]*)`)
	dsnHostRegex    = regexp.MustCompile(`(?i)(?:host|server)\s*=\s*['"]?([^\s;'"]+)`)
)

// ValidateConnectionTarget verifies that the database connection target (host/IP)
// does not point to localhost, private networks, loopbacks, unix domain sockets, or cloud metadata services.
func ValidateConnectionTarget(dsn string, allowPrivate bool) error {
	trimmed := strings.TrimSpace(dsn)
	if trimmed == "" {
		return errors.New("empty dsn")
	}

	if allowPrivate {
		return nil
	}

	var host string
	if strings.Contains(trimmed, "://") {
		u, err := url.Parse(trimmed)
		if err == nil {
			if u.Scheme == "mock" {
				return nil
			}
			// Check URL query parameters for host overrides (e.g. ?host=/var/run/postgresql)
			if qHost := u.Query().Get("host"); qHost != "" {
				if strings.HasPrefix(qHost, "/") || strings.Contains(qHost, "/") || strings.EqualFold(qHost, "localhost") {
					return fmt.Errorf("%w: query parameter host %q is prohibited", ErrBlockedSQLConnection, qHost)
				}
				host = qHost
			} else {
				host = u.Hostname()
			}
		}
	}

	if host == "" {
		if m := dsnHostRegex.FindStringSubmatch(trimmed); len(m) > 1 {
			host = m[1]
		}
	}

	// Any connection string with no host or targeting local files/sockets is blocked
	if host == "" {
		return fmt.Errorf("%w: remote database host must be explicitly specified (local sockets and local file connections are prohibited)", ErrBlockedSQLConnection)
	}

	// Reject Unix domain sockets and relative/absolute filesystem paths
	if strings.HasPrefix(host, "/") || strings.Contains(host, "/") || strings.HasPrefix(host, ".") {
		return fmt.Errorf("%w: unix domain socket and relative path connection targets (%q) are prohibited", ErrBlockedSQLConnection, host)
	}

	hostLower := strings.ToLower(strings.Trim(host, "[]"))
	if _, blocked := blockedHostnames[hostLower]; blocked {
		return fmt.Errorf("%w: connection target %q is explicitly blocked", ErrBlockedSQLConnection, host)
	}

	// Check if host is direct IP address
	if ip, err := netip.ParseAddr(hostLower); err == nil {
		unmapped := ip.Unmap()
		if unmapped.IsLoopback() || unmapped.IsPrivate() || unmapped.IsLinkLocalUnicast() || unmapped.IsLinkLocalMulticast() || unmapped.IsUnspecified() {
			return fmt.Errorf("%w: target IP %s is a loopback or private network address", ErrBlockedSQLConnection, ip)
		}
		return nil
	}

	// Resolve hostname to check IPs
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", hostLower)
	if err != nil {
		// If DNS resolution fails, fail closed for security
		return fmt.Errorf("%w: failed to resolve host %q: %v", ErrBlockedSQLConnection, host, err)
	}

	if len(ips) == 0 {
		return fmt.Errorf("%w: no IPs found for host %q", ErrBlockedSQLConnection, host)
	}

	for _, ip := range ips {
		unmapped := ip.Unmap()
		if unmapped.IsLoopback() || unmapped.IsPrivate() || unmapped.IsLinkLocalUnicast() || unmapped.IsLinkLocalMulticast() || unmapped.IsUnspecified() {
			return fmt.Errorf("%w: host %q resolves to prohibited IP %s", ErrBlockedSQLConnection, host, ip)
		}
	}

	return nil
}

// SQLConnectionPool manages reusable pooled database connections.
type SQLConnectionPool struct {
	mu    sync.RWMutex
	pools map[string]*sql.DB
}

var defaultSQLPool = NewSQLConnectionPool()

// DefaultSQLConnectionPool returns the process-wide default SQL connection pool.
func DefaultSQLConnectionPool() *SQLConnectionPool {
	return defaultSQLPool
}

// NewSQLConnectionPool creates a new connection pool manager.
func NewSQLConnectionPool() *SQLConnectionPool {
	return &SQLConnectionPool{
		pools: make(map[string]*sql.DB),
	}
}

// Get returns or opens a pooled *sql.DB for the given driver and DSN.
func (p *SQLConnectionPool) Get(driverName, dsn string) (*sql.DB, error) {
	if driverName == "" {
		driverName = "pgx"
	}
	key := driverName + "://" + dsn

	p.mu.RLock()
	db, ok := p.pools[key]
	p.mu.RUnlock()
	if ok {
		return db, nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Double check
	if db, ok := p.pools[key]; ok {
		return db, nil
	}

	var newDB *sql.DB
	var err error
	switch driverName {
	case "pgx", "postgres":
		connConfig, parseErr := pgx.ParseConfig(dsn)
		if parseErr != nil {
			return nil, fmt.Errorf("parse postgres dsn: %w", parseErr)
		}
		sd := NewSafeDialer(SafeDialerConfig{})
		connConfig.DialFunc = sd.DialContext
		newDB = stdlib.OpenDB(*connConfig)
	case "sqlite", "sqlite3", "mock_sql_driver":
		newDB, err = sql.Open(driverName, dsn)
		if err != nil {
			return nil, fmt.Errorf("open database: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported database driver %q: only postgres/pgx and sqlite are supported", driverName)
	}

	newDB.SetMaxOpenConns(10)
	newDB.SetMaxIdleConns(2)
	newDB.SetConnMaxLifetime(5 * time.Minute)

	p.pools[key] = newDB
	return newDB, nil
}

// Close closes all pooled database connections.
func (p *SQLConnectionPool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var firstErr error
	for _, db := range p.pools {
		if err := db.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	p.pools = make(map[string]*sql.DB)
	return firstErr
}

// SQLTemplateExecutor executes parameterized read-only SQL queries.
type SQLTemplateExecutor struct {
	def          core.ToolDefinition
	pool         *SQLConnectionPool
	driver       string
	dsn          string
	allowPrivate bool
}

// NewSQLTemplateExecutor creates a new SQLTemplateExecutor for the tool definition with private networks blocked.
func NewSQLTemplateExecutor(def core.ToolDefinition, pool *SQLConnectionPool) *SQLTemplateExecutor {
	return NewSQLTemplateExecutorWithPrivate(def, pool, false)
}

// NewSQLTemplateExecutorWithPrivate creates a new SQLTemplateExecutor with configurable private network permission.
func NewSQLTemplateExecutorWithPrivate(def core.ToolDefinition, pool *SQLConnectionPool, allowPrivate bool) *SQLTemplateExecutor {
	if pool == nil {
		pool = DefaultSQLConnectionPool()
	}

	driver := def.Executor.Method // e.g. "pgx", "postgres"
	if driver == "" {
		driver = "pgx"
	}

	dsn := def.Executor.URL
	if dsn == "" && def.Executor.Headers != nil {
		dsn = def.Executor.Headers["dsn"]
	}

	return &SQLTemplateExecutor{
		def:          def,
		pool:         pool,
		driver:       driver,
		dsn:          dsn,
		allowPrivate: allowPrivate,
	}
}

// ValidateReadOnlySQL checks that the given SQL string is strictly read-only.
func ValidateReadOnlySQL(template string) error {
	stripped := sqlCommentRegex.ReplaceAllString(template, " ")
	trimmed := strings.TrimSpace(stripped)
	if trimmed == "" {
		return fmt.Errorf("%w: empty query", ErrInvalidSQLTemplate)
	}

	// Remove trailing semicolon if any
	trimmed = strings.TrimSuffix(trimmed, ";")
	trimmed = strings.TrimSpace(trimmed)

	// Disallow multiple statements separated by semicolons
	if strings.Contains(trimmed, ";") {
		return fmt.Errorf("%w: multiple statements prohibited", ErrMutatingSQLRejected)
	}

	// Must begin with SELECT, WITH, or EXPLAIN
	upper := strings.ToUpper(trimmed)
	fields := strings.Fields(upper)
	if len(fields) == 0 {
		return fmt.Errorf("%w: empty query after trimming", ErrInvalidSQLTemplate)
	}

	firstWord := fields[0]
	if firstWord != "SELECT" && firstWord != "WITH" && firstWord != "EXPLAIN" {
		return fmt.Errorf("%w: statement must start with SELECT or WITH (got %q)", ErrMutatingSQLRejected, firstWord)
	}

	// Block mutating keywords and dangerous server-side functions anywhere in statement tokens
	prohibitedTokens := map[string]struct{}{
		"INSERT":               {},
		"UPDATE":               {},
		"DELETE":               {},
		"DROP":                 {},
		"ALTER":                {},
		"TRUNCATE":             {},
		"GRANT":                {},
		"REVOKE":               {},
		"CREATE":               {},
		"REPLACE":              {},
		"EXEC":                 {},
		"EXECUTE":              {},
		"CALL":                 {},
		"SET":                  {},
		"BEGIN":                {},
		"COMMIT":               {},
		"ROLLBACK":             {},
		"COPY":                 {},
		"PG_READ_FILE":         {},
		"PG_READ_BINARY_FILE":  {},
		"PG_WRITE_FILE":        {},
		"LO_EXPORT":            {},
		"LO_IMPORT":            {},
		"DBLINK_EXEC":          {},
		"PG_TERMINATE_BACKEND": {},
		"PG_CANCEL_BACKEND":    {},
	}

	tokens := strings.FieldsFunc(upper, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '(' || r == ')' || r == ',' || r == '.' || r == ';'
	})

	for _, token := range tokens {
		if _, prohibited := prohibitedTokens[token]; prohibited {
			return fmt.Errorf("%w: token %q is not permitted in read-only templates", ErrMutatingSQLRejected, token)
		}
	}

	return nil
}

// PrepareParameterizedQuery converts named parameters (:param) to positional placeholders ($1, $2 or ?).
func PrepareParameterizedQuery(template, driver string, args map[string]any, id core.Identity) (string, []any, error) {
	if err := ValidateReadOnlySQL(template); err != nil {
		return "", nil, err
	}

	// Populate args and strictly enforce confused-deputy defense:
	// Server-validated identity claims MUST overwrite caller-supplied arguments (Rule 6.2).
	mergedArgs := make(map[string]any, len(args)+2)
	for k, v := range args {
		mergedArgs[k] = v
	}
	if id.Subject != "" {
		mergedArgs["user_id"] = id.Subject
		mergedArgs["subject"] = id.Subject
	}

	var (
		paramValues []any
		paramIndex  int
		isPostgres  = driver == "pgx" || driver == "postgres"
	)

	transformedQuery := namedParamRegex.ReplaceAllStringFunc(template, func(match string) string {
		sub := namedParamRegex.FindStringSubmatch(match)
		if len(sub) < 3 {
			return match
		}
		prefix := sub[1]
		paramName := sub[2]
		val, exists := mergedArgs[paramName]
		if !exists {
			// Pass nil for missing optional parameters
			val = nil
		}

		paramValues = append(paramValues, val)
		paramIndex++

		if isPostgres {
			return fmt.Sprintf("%s$%d", prefix, paramIndex)
		}
		return fmt.Sprintf("%s?", prefix)
	})

	return transformedQuery, paramValues, nil
}

// Execute runs the parameterized query against the configured database.
func (e *SQLTemplateExecutor) Execute(ctx context.Context, call core.ToolCall, id core.Identity) (core.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  e.def.ID,
			Content: err.Error(),
			IsError: true,
		}, err
	}

	template := e.def.Executor.Template
	if strings.TrimSpace(template) == "" {
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  e.def.ID,
			Content: "sql template is empty",
			IsError: true,
		}, fmt.Errorf("%w: empty template", ErrInvalidSQLTemplate)
	}

	var args map[string]any
	if strings.TrimSpace(call.Arguments) != "" {
		if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
			return core.ToolResult{
				CallID:  call.ID,
				ToolID:  e.def.ID,
				Content: fmt.Sprintf("invalid arguments JSON: %v", err),
				IsError: true,
			}, fmt.Errorf("%w: invalid arguments: %v", ErrToolExecution, err)
		}
	} else {
		args = make(map[string]any)
	}

	timeout := time.Duration(e.def.Executor.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if e.dsn == "" {
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  e.def.ID,
			Content: "database connection string (DSN) is empty",
			IsError: true,
		}, fmt.Errorf("%w: empty dsn", ErrToolExecution)
	}

	if err := ValidateConnectionTarget(e.dsn, e.allowPrivate); err != nil {
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  e.def.ID,
			Content: fmt.Sprintf("database connection blocked by security policy: %v", err),
			IsError: true,
		}, err
	}

	query, paramValues, err := PrepareParameterizedQuery(template, e.driver, args, id)
	if err != nil {
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  e.def.ID,
			Content: fmt.Sprintf("prepare query error: %v", err),
			IsError: true,
		}, err
	}

	db, err := e.pool.Get(e.driver, e.dsn)
	if err != nil {
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  e.def.ID,
			Content: fmt.Sprintf("connect database error: %v", err),
			IsError: true,
		}, err
	}

	rows, err := db.QueryContext(execCtx, query, paramValues...)
	if err != nil {
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  e.def.ID,
			Content: fmt.Sprintf("query error: %v", err),
			IsError: true,
		}, err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  e.def.ID,
			Content: fmt.Sprintf("get columns error: %v", err),
			IsError: true,
		}, err
	}

	var results []map[string]any
	colCount := len(columns)
	const maxSQLRows = 1000
	const maxSQLResultBytes = 2 << 20 // 2MB
	var accumulatedBytes int

	for rows.Next() {
		if len(results) >= maxSQLRows {
			return core.ToolResult{
				CallID:  call.ID,
				ToolID:  e.def.ID,
				Content: fmt.Sprintf("query returned too many rows (limit %d exceeded)", maxSQLRows),
				IsError: true,
			}, fmt.Errorf("sql query row limit %d exceeded", maxSQLRows)
		}

		values := make([]any, colCount)
		valuePtrs := make([]any, colCount)
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return core.ToolResult{
				CallID:  call.ID,
				ToolID:  e.def.ID,
				Content: fmt.Sprintf("scan row error: %v", err),
				IsError: true,
			}, err
		}

		rowMap := make(map[string]any, colCount)
		for i, col := range columns {
			val := values[i]
			if b, ok := val.([]byte); ok {
				val = string(b)
				accumulatedBytes += len(b)
			} else if s, ok := val.(string); ok {
				accumulatedBytes += len(s)
			} else {
				accumulatedBytes += 8
			}
			rowMap[col] = val
		}
		if accumulatedBytes > maxSQLResultBytes {
			return core.ToolResult{
				CallID:  call.ID,
				ToolID:  e.def.ID,
				Content: fmt.Sprintf("query result size exceeded maximum limit (%d bytes)", maxSQLResultBytes),
				IsError: true,
			}, fmt.Errorf("sql query result byte limit %d exceeded", maxSQLResultBytes)
		}
		results = append(results, rowMap)
	}

	if err := rows.Err(); err != nil {
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  e.def.ID,
			Content: fmt.Sprintf("row iteration error: %v", err),
			IsError: true,
		}, err
	}

	if results == nil {
		results = []map[string]any{}
	}

	resBytes, err := json.Marshal(results)
	if err != nil {
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  e.def.ID,
			Content: fmt.Sprintf("marshal results error: %v", err),
			IsError: true,
		}, err
	}

	return core.ToolResult{
		CallID:  call.ID,
		ToolID:  e.def.ID,
		Content: string(resBytes),
		IsError: false,
		Metadata: map[string]any{
			"row_count": len(results),
			"columns":   columns,
			"driver":    e.driver,
		},
	}, nil
}
