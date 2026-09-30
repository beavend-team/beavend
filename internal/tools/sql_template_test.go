package tools

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"io"
	"sync"
	"testing"

	"github.com/beavend/beavend/internal/core"
)

func TestValidateReadOnlySQL(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		shouldErr bool
	}{
		{
			name:      "simple select",
			query:     "SELECT id, name FROM users WHERE id = :id",
			shouldErr: false,
		},
		{
			name:      "select with comments",
			query:     "/* user query */ SELECT id, email -- comment\nFROM users WHERE id = :id;",
			shouldErr: false,
		},
		{
			name:      "cte with select",
			query:     "WITH recent_orders AS (SELECT * FROM orders WHERE user_id = :user_id) SELECT * FROM recent_orders",
			shouldErr: false,
		},
		{
			name:      "explain select",
			query:     "EXPLAIN SELECT * FROM users",
			shouldErr: false,
		},
		{
			name:      "reject insert",
			query:     "INSERT INTO users (id, name) VALUES (1, 'alice')",
			shouldErr: true,
		},
		{
			name:      "reject update",
			query:     "UPDATE users SET name = 'bob' WHERE id = 1",
			shouldErr: true,
		},
		{
			name:      "reject delete",
			query:     "DELETE FROM users WHERE id = 1",
			shouldErr: true,
		},
		{
			name:      "reject drop",
			query:     "DROP TABLE users",
			shouldErr: true,
		},
		{
			name:      "reject multi-statement query",
			query:     "SELECT * FROM users; DROP TABLE users;",
			shouldErr: true,
		},
		{
			name:      "reject sub-insert or grant",
			query:     "SELECT * FROM users WHERE id IN (SELECT id FROM accounts); GRANT ALL ON users TO public;",
			shouldErr: true,
		},
		{
			name:      "empty query",
			query:     "   \n\t  ",
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateReadOnlySQL(tt.query)
			if tt.shouldErr && err == nil {
				t.Errorf("expected error for query %q, got nil", tt.query)
			}
			if !tt.shouldErr && err != nil {
				t.Errorf("unexpected error for query %q: %v", tt.query, err)
			}
		})
	}
}

func TestPrepareParameterizedQuery(t *testing.T) {
	template := "SELECT id, status, total FROM orders WHERE user_id = :user_id AND status = :status LIMIT :limit"

	t.Run("postgres dialect", func(t *testing.T) {
		args := map[string]any{
			"status": "completed",
			"limit":  10,
		}
		id := core.Identity{Subject: "usr_42"}

		query, params, err := PrepareParameterizedQuery(template, "postgres", args, id)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expectedQuery := "SELECT id, status, total FROM orders WHERE user_id = $1 AND status = $2 LIMIT $3"
		if query != expectedQuery {
			t.Errorf("expected query %q, got %q", expectedQuery, query)
		}

		if len(params) != 3 {
			t.Fatalf("expected 3 params, got %d", len(params))
		}
		if params[0] != "usr_42" {
			t.Errorf("expected param 0 to be 'usr_42', got %v", params[0])
		}
		if params[1] != "completed" {
			t.Errorf("expected param 1 to be 'completed', got %v", params[1])
		}
		if params[2] != 10 {
			t.Errorf("expected param 2 to be 10, got %v", params[2])
		}
	})

	t.Run("sqlite or mysql dialect", func(t *testing.T) {
		args := map[string]any{
			"status": "pending",
			"limit":  5,
		}
		id := core.Identity{Subject: "usr_99"}

		query, params, err := PrepareParameterizedQuery(template, "mysql", args, id)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expectedQuery := "SELECT id, status, total FROM orders WHERE user_id = ? AND status = ? LIMIT ?"
		if query != expectedQuery {
			t.Errorf("expected query %q, got %q", expectedQuery, query)
		}

		if len(params) != 3 {
			t.Fatalf("expected 3 params, got %d", len(params))
		}
		if params[0] != "usr_99" {
			t.Errorf("expected param 0 to be 'usr_99', got %v", params[0])
		}
	})
}

// Mock SQL Driver to test SQLTemplateExecutor execution without live network dependencies
type mockSQLDriver struct{}

func (m *mockSQLDriver) Open(name string) (driver.Conn, error) {
	return &mockSQLConn{}, nil
}

type mockSQLConn struct{}

func (c *mockSQLConn) Prepare(query string) (driver.Stmt, error) {
	return &mockSQLStmt{query: query}, nil
}

func (c *mockSQLConn) Close() error { return nil }
func (c *mockSQLConn) Begin() (driver.Tx, error) {
	return nil, sql.ErrTxDone
}

type mockSQLStmt struct {
	query string
}

func (s *mockSQLStmt) Close() error  { return nil }
func (s *mockSQLStmt) NumInput() int { return -1 }

func (s *mockSQLStmt) Exec(args []driver.Value) (driver.Result, error) {
	return nil, nil
}

func (s *mockSQLStmt) Query(args []driver.Value) (driver.Rows, error) {
	return &mockSQLRows{
		columns: []string{"id", "status", "total"},
		data: [][]driver.Value{
			{"ord_1", "delivered", 49.99},
			{"ord_2", "shipped", 19.50},
		},
	}, nil
}

type mockSQLRows struct {
	columns []string
	data    [][]driver.Value
	idx     int
}

func (r *mockSQLRows) Columns() []string { return r.columns }
func (r *mockSQLRows) Close() error      { return nil }
func (r *mockSQLRows) Next(dest []driver.Value) error {
	if r.idx >= len(r.data) {
		return io.EOF
	}
	row := r.data[r.idx]
	for i, val := range row {
		dest[i] = val
	}
	r.idx++
	return nil
}

var registerOnce sync.Once

func registerMockDriver() {
	registerOnce.Do(func() {
		sql.Register("mock_sql_driver", &mockSQLDriver{})
	})
}

func TestSQLTemplateExecutor_Execution(t *testing.T) {
	registerMockDriver()

	toolDef := core.ToolDefinition{
		ID:   "get_orders",
		Name: "get_orders",
		Executor: core.ExecutorConfig{
			Type:      "sql_template",
			Method:    "mock_sql_driver",
			URL:       "mock://orders_db",
			Template:  "SELECT id, status, total FROM orders WHERE user_id = :user_id",
			TimeoutMS: 5000,
		},
	}

	exec := NewSQLTemplateExecutor(toolDef, nil)
	call := core.ToolCall{
		ID:        "call_sql_1",
		Name:      "get_orders",
		Arguments: `{}`,
	}

	id := core.Identity{Subject: "user_alice"}
	result, err := exec.Execute(context.Background(), call, id)
	if err != nil {
		t.Fatalf("unexpected execution error: %v", err)
	}

	if result.IsError {
		t.Fatalf("expected non-error result, got error: %s", result.Content)
	}

	var rows []map[string]any
	if err := json.Unmarshal([]byte(result.Content), &rows); err != nil {
		t.Fatalf("failed to parse result json: %v", err)
	}

	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	if rows[0]["id"] != "ord_1" || rows[0]["status"] != "delivered" {
		t.Errorf("unexpected row 0 content: %v", rows[0])
	}
	if rows[1]["id"] != "ord_2" || rows[1]["status"] != "shipped" {
		t.Errorf("unexpected row 1 content: %v", rows[1])
	}

	if result.Metadata["row_count"] != 2 {
		t.Errorf("expected row_count 2, got %v", result.Metadata["row_count"])
	}
}
