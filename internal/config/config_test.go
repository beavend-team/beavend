package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConfig_DefaultValues(t *testing.T) {
	emptyLookup := func(_ string) string { return "" }
	cfg, err := LoadWithLookup(emptyLookup)
	if err != nil {
		t.Fatalf("expected LoadWithLookup to succeed with defaults, got: %v", err)
	}

	if cfg.Env != EnvDevelopment {
		t.Errorf("expected default Env to be %s, got %s", EnvDevelopment, cfg.Env)
	}
	if cfg.Role != RoleAll {
		t.Errorf("expected default Role to be %s, got %s", RoleAll, cfg.Role)
	}
	if cfg.HTTP.Port != 8080 {
		t.Errorf("expected default HTTP port 8080, got %d", cfg.HTTP.Port)
	}
	if cfg.HTTP.ReadTimeout != 15*time.Second {
		t.Errorf("expected default ReadTimeout 15s, got %v", cfg.HTTP.ReadTimeout)
	}
}

func TestConfig_EnvOverrides(t *testing.T) {
	envMap := map[string]string{
		"CORTEXBASE_ENV":               "staging",
		"CORTEXBASE_ROLE":              "runtime",
		"CORTEXBASE_HTTP_PORT":         "9000",
		"CORTEXBASE_HTTP_READ_TIMEOUT": "30s",
		"CORTEXBASE_DATABASE_URL":      "postgres://user:pass@localhost:5432/cortexbase_test",
		"CORTEXBASE_REDIS_URL":         "redis://localhost:6380/1",
		"CORTEXBASE_MASTER_KEY":        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"CORTEXBASE_ADMIN_EMAIL":       "ops@company.com",
		"CORTEXBASE_ADMIN_PASSWORD":    "supersecretpass",
	}

	lookup := func(k string) string {
		return envMap[k]
	}

	cfg, err := LoadWithLookup(lookup)
	if err != nil {
		t.Fatalf("expected success with valid overrides, got: %v", err)
	}

	if cfg.Env != EnvStaging {
		t.Errorf("expected Env %s, got %s", EnvStaging, cfg.Env)
	}
	if cfg.Role != RoleRuntime {
		t.Errorf("expected Role %s, got %s", RoleRuntime, cfg.Role)
	}
	if cfg.HTTP.Port != 9000 {
		t.Errorf("expected HTTP port 9000, got %d", cfg.HTTP.Port)
	}
	if cfg.HTTP.ReadTimeout != 30*time.Second {
		t.Errorf("expected ReadTimeout 30s, got %v", cfg.HTTP.ReadTimeout)
	}
}

func TestConfig_YAMLFileLoading(t *testing.T) {
	tempDir := t.TempDir()
	yamlPath := filepath.Join(tempDir, "config.yaml")

	yamlContent := `
env: staging
role: control
http:
  port: 8443
  read_timeout: 20s
database:
  url: postgres://postgres:secret@localhost:5432/cortex_yaml?sslmode=disable
`
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0600); err != nil {
		t.Fatalf("failed to write test YAML config: %v", err)
	}

	// 1. Load solely from YAML without env overrides
	emptyLookup := func(_ string) string { return "" }
	cfg, err := LoadFileWithLookup(yamlPath, emptyLookup)
	if err != nil {
		t.Fatalf("failed to load YAML config: %v", err)
	}

	if cfg.Env != EnvStaging {
		t.Errorf("expected Env 'staging', got %s", cfg.Env)
	}
	if cfg.Role != RoleControl {
		t.Errorf("expected Role 'control', got %s", cfg.Role)
	}
	if cfg.HTTP.Port != 8443 {
		t.Errorf("expected Port 8443, got %d", cfg.HTTP.Port)
	}
	if cfg.HTTP.ReadTimeout != 20*time.Second {
		t.Errorf("expected ReadTimeout 20s, got %v", cfg.HTTP.ReadTimeout)
	}

	// 2. Load with environment overriding YAML values
	overrideLookup := func(k string) string {
		if k == "CORTEXBASE_HTTP_PORT" {
			return "9999"
		}
		return ""
	}
	cfgOverridden, err := LoadFileWithLookup(yamlPath, overrideLookup)
	if err != nil {
		t.Fatalf("failed to load YAML config with env override: %v", err)
	}
	if cfgOverridden.HTTP.Port != 9999 {
		t.Errorf("expected env to override YAML port to 9999, got %d", cfgOverridden.HTTP.Port)
	}
}

func TestConfig_ValidationErrors(t *testing.T) {
	tests := []struct {
		name      string
		envMap    map[string]string
		errSubstr string
	}{
		{
			name: "invalid role",
			envMap: map[string]string{
				"CORTEXBASE_ROLE": "invalid_role",
			},
			errSubstr: "invalid role",
		},
		{
			name: "invalid environment",
			envMap: map[string]string{
				"CORTEXBASE_ENV": "sandbox",
			},
			errSubstr: "invalid environment",
		},
		{
			name: "invalid http port",
			envMap: map[string]string{
				"CORTEXBASE_HTTP_PORT": "70000",
			},
			errSubstr: "invalid HTTP port",
		},
		{
			name: "invalid master key length",
			envMap: map[string]string{
				"CORTEXBASE_MASTER_KEY": "tooshort",
			},
			errSubstr: "security master key must be a valid 64-character",
		},
		{
			name: "malformed database url",
			envMap: map[string]string{
				"CORTEXBASE_DATABASE_URL": "mysql://user:pass@localhost/db",
			},
			errSubstr: "malformed database connection string",
		},
		{
			name: "malformed redis url",
			envMap: map[string]string{
				"CORTEXBASE_REDIS_URL": "http://localhost:6379",
			},
			errSubstr: "malformed redis connection string",
		},
		{
			name: "production with default master key",
			envMap: map[string]string{
				"CORTEXBASE_ENV": "production",
			},
			errSubstr: "default master key is forbidden in production",
		},
		{
			name: "production with default admin password",
			envMap: map[string]string{
				"CORTEXBASE_ENV":        "production",
				"CORTEXBASE_MASTER_KEY": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			},
			errSubstr: "default admin password is forbidden in production",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lookup := func(k string) string { return tc.envMap[k] }
			_, err := LoadWithLookup(lookup)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.errSubstr)
			}
			if !strings.Contains(err.Error(), tc.errSubstr) {
				t.Errorf("expected error to contain %q, got %q", tc.errSubstr, err.Error())
			}
		})
	}
}
