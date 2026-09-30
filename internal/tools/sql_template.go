package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx" driver

	"github.com/beavend/beavend/internal/core"
)

var (
	// ErrMutatingSQLRejected is returned when a SQL template contains mutating or prohibited statements.
	ErrMutatingSQLRejected = errors.New("sql template contains mutating or prohibited statement")
	// ErrInvalidSQLTemplate is returned when the template syntax or parameters are malformed.
	ErrInvalidSQLTemplate = errors.New("invalid sql template")

	namedParamRegex = regexp.MustCompile(`:([a-zA-Z_][a-zA-Z0-9_]*)`)
	sqlCommentRegex = regexp.MustCompile(`(?s)(/\*.*?\*/)|(--[^\r\n]*)`)
)

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

	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)

	p.pools[key] = db
	return db, nil
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
	def    core.ToolDefinition
	pool   *SQLConnectionPool
	driver string
	dsn    string
}

// NewSQLTemplateExecutor creates a new SQLTemplateExecutor for the tool definition.
func NewSQLTemplateExecutor(def core.ToolDefinition, pool *SQLConnectionPool) *SQLTemplateExecutor {
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
		def:    def,
		pool:   pool,
		driver: driver,
		dsn:    dsn,
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

	// Block mutating keywords anywhere in the statement tokens
	prohibitedTokens := map[string]struct{}{
		"INSERT":   {},
		"UPDATE":   {},
		"DELETE":   {},
		"DROP":     {},
		"ALTER":    {},
		"TRUNCATE": {},
		"GRANT":    {},
		"REVOKE":   {},
		"CREATE":   {},
		"REPLACE":  {},
		"EXEC":     {},
		"EXECUTE":  {},
		"CALL":     {},
		"SET":      {},
		"BEGIN":    {},
		"COMMIT":   {},
		"ROLLBACK": {},
		"COPY":     {},
	}

	tokens := strings.FieldsFunc(upper, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '(' || r == ')' || r == ','
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

	// Populate identity parameters into args if not explicitly overridden
	mergedArgs := make(map[string]any, len(args)+2)
	for k, v := range args {
		mergedArgs[k] = v
	}
	if _, exists := mergedArgs["user_id"]; !exists && id.Subject != "" {
		mergedArgs["user_id"] = id.Subject
	}
	if _, exists := mergedArgs["subject"]; !exists && id.Subject != "" {
		mergedArgs["subject"] = id.Subject
	}

	var (
		paramValues []any
		paramIndex  int
		isPostgres  = driver == "pgx" || driver == "postgres"
	)

	transformedQuery := namedParamRegex.ReplaceAllStringFunc(template, func(match string) string {
		paramName := match[1:] // remove leading ':'
		val, exists := mergedArgs[paramName]
		if !exists {
			// Pass nil for missing optional parameters
			val = nil
		}

		paramValues = append(paramValues, val)
		paramIndex++

		if isPostgres {
			return fmt.Sprintf("$%d", paramIndex)
		}
		return "?"
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

	for rows.Next() {
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
			}
			rowMap[col] = val
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
