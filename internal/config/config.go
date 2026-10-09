package config

import (
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Role constants.
const (
	RoleAll     = "all"
	RoleRuntime = "runtime"
	RoleControl = "control"
)

// Environment constants.
const (
	EnvDevelopment = "development"
	EnvStaging     = "staging"
	EnvProduction  = "production"
)

// Config holds all server configuration settings.
type Config struct {
	Env      string         `json:"env" yaml:"env"`
	Role     string         `json:"role" yaml:"role"`
	HTTP     HTTPConfig     `json:"http" yaml:"http"`
	Database DatabaseConfig `json:"database" yaml:"database"`
	Redis    RedisConfig    `json:"redis" yaml:"redis"`
	LiteLLM  LiteLLMConfig  `json:"litellm" yaml:"litellm"`
	Security SecurityConfig `json:"security" yaml:"security"`
	Admin    AdminConfig    `json:"admin" yaml:"admin"`
	Log      LogConfig      `json:"log" yaml:"log"`
	Usage    UsageConfig    `json:"usage" yaml:"usage"`
	Search   SearchConfig   `json:"search" yaml:"search"`
	Pricing  PricingConfig  `json:"pricing" yaml:"pricing"`
}

// PricingConfig holds model pricing and background catalog synchronization settings.
type PricingConfig struct {
	AutoSync     bool          `json:"auto_sync" yaml:"auto_sync"`
	CatalogURL   string        `json:"catalog_url" yaml:"catalog_url"`
	SyncInterval time.Duration `json:"sync_interval" yaml:"sync_interval"`
}

// SearchConfig holds SearXNG search engine integration settings.
type SearchConfig struct {
	SearxngURL string        `json:"searxng_url" yaml:"searxng_url"`
	Enabled    bool          `json:"enabled" yaml:"enabled"`
	Timeout    time.Duration `json:"timeout" yaml:"timeout"`
}

// HTTPConfig holds web server settings.
type HTTPConfig struct {
	Port         int           `json:"port" yaml:"port"`
	ReadTimeout  time.Duration `json:"read_timeout" yaml:"read_timeout"`
	WriteTimeout time.Duration `json:"write_timeout" yaml:"write_timeout"`
	IdleTimeout  time.Duration `json:"idle_timeout" yaml:"idle_timeout"`
}

// DatabaseConfig holds PostgreSQL configuration.
type DatabaseConfig struct {
	URL             string        `json:"-" yaml:"url"` // Don't serialize credentials in JSON
	MaxOpenConns    int           `json:"max_open_conns" yaml:"max_open_conns"`
	MaxIdleConns    int           `json:"max_idle_conns" yaml:"max_idle_conns"`
	ConnMaxLifetime time.Duration `json:"conn_max_lifetime" yaml:"conn_max_lifetime"`
}

// RedisConfig holds Redis/Valkey configuration.
type RedisConfig struct {
	URL        string `json:"-" yaml:"url"` // Don't serialize credentials in JSON
	PoolSize   int    `json:"pool_size" yaml:"pool_size"`
	FailClosed bool   `json:"fail_closed" yaml:"fail_closed"`
}

// LiteLLMConfig holds LiteLLM gateway configuration.
type LiteLLMConfig struct {
	URL string `json:"url" yaml:"url"`
	Key string `json:"-" yaml:"key"` // Don't serialize secret in JSON
}

// SecurityConfig holds encryption and secret settings.
type SecurityConfig struct {
	MasterKey string `json:"-" yaml:"master_key"` // 32-byte hex string
}

// AdminConfig holds initial control plane credentials.
type AdminConfig struct {
	Email    string `json:"email" yaml:"email"`
	Password string `json:"-" yaml:"password"`
}

// LogConfig holds logging configuration.
type LogConfig struct {
	Level  string `json:"level" yaml:"level"`
	Format string `json:"format" yaml:"format"`
}

// UsageConfig holds usage tracking and async logging settings.
type UsageConfig struct {
	BufferSize     int           `json:"buffer_size" yaml:"buffer_size"`
	BatchSize      int           `json:"batch_size" yaml:"batch_size"`
	FlushInterval  time.Duration `json:"flush_interval" yaml:"flush_interval"`
	RollupInterval time.Duration `json:"rollup_interval" yaml:"rollup_interval"`
}

// DefaultConfig returns a new Config with standard development defaults.
func DefaultConfig() *Config {
	return &Config{
		Env:  EnvDevelopment,
		Role: RoleAll,
		HTTP: HTTPConfig{
			Port:         8080,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 60 * time.Second,
			IdleTimeout:  120 * time.Second,
		},
		Database: DatabaseConfig{
			URL:             "postgres://beavend:beavend@localhost:5432/beavend?sslmode=disable",
			MaxOpenConns:    25,
			MaxIdleConns:    5,
			ConnMaxLifetime: 15 * time.Minute,
		},
		Redis: RedisConfig{
			URL:        "redis://localhost:6379/0",
			PoolSize:   50,
			FailClosed: true,
		},
		LiteLLM: LiteLLMConfig{
			URL: "http://localhost:4000",
			Key: "sk-beavend-local-master",
		},
		Security: SecurityConfig{
			MasterKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		},
		Admin: AdminConfig{
			Email:    "admin@beavend.local",
			Password: "adminpassword123",
		},
		Log: LogConfig{
			Level:  "info",
			Format: "json",
		},
		Usage: UsageConfig{
			BufferSize:     10000,
			BatchSize:      100,
			FlushInterval:  500 * time.Millisecond,
			RollupInterval: 1 * time.Minute,
		},
		Search: SearchConfig{
			SearxngURL: "http://localhost:8081",
			Enabled:    true,
			Timeout:    8 * time.Second,
		},
		Pricing: PricingConfig{
			AutoSync:     false,
			CatalogURL:   "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json",
			SyncInterval: 24 * time.Hour,
		},
	}
}

// Load loads configuration from environment variables with sensible defaults.
func Load() (*Config, error) {
	return LoadWithLookup(os.Getenv)
}

// LoadFile loads configuration from a YAML file, merging with environment overrides.
func LoadFile(filePath string) (*Config, error) {
	return LoadFileWithLookup(filePath, os.Getenv)
}

// LoadWithLookup loads configuration using a custom lookup function (for testing).
func LoadWithLookup(lookup func(string) string) (*Config, error) {
	return LoadFileWithLookup("", lookup)
}

// LoadFileWithLookup loads configuration from a YAML file (if provided) and environment overrides.
func LoadFileWithLookup(filePath string, lookup func(string) string) (*Config, error) {
	cfg := DefaultConfig()

	// If YAML file path is specified, load and unmarshal
	if filePath != "" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read config file %q: %w", filePath, err)
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("failed to parse YAML config file: %w", err)
		}
	}

	// Apply environment variable overrides if present
	if val := lookup("BEAVEND_ENV"); val != "" {
		cfg.Env = strings.TrimSpace(val)
	}
	if val := lookup("BEAVEND_ROLE"); val != "" {
		cfg.Role = strings.TrimSpace(val)
	}

	if val := lookup("BEAVEND_HTTP_PORT"); val != "" {
		port, err := strconv.Atoi(strings.TrimSpace(val))
		if err != nil {
			return nil, fmt.Errorf("invalid BEAVEND_HTTP_PORT: %w", err)
		}
		cfg.HTTP.Port = port
	}

	if val := lookup("BEAVEND_HTTP_READ_TIMEOUT"); val != "" {
		d, err := time.ParseDuration(strings.TrimSpace(val))
		if err != nil {
			return nil, fmt.Errorf("invalid BEAVEND_HTTP_READ_TIMEOUT: %w", err)
		}
		cfg.HTTP.ReadTimeout = d
	}

	if val := lookup("BEAVEND_HTTP_WRITE_TIMEOUT"); val != "" {
		d, err := time.ParseDuration(strings.TrimSpace(val))
		if err != nil {
			return nil, fmt.Errorf("invalid BEAVEND_HTTP_WRITE_TIMEOUT: %w", err)
		}
		cfg.HTTP.WriteTimeout = d
	}

	if val := lookup("BEAVEND_HTTP_IDLE_TIMEOUT"); val != "" {
		d, err := time.ParseDuration(strings.TrimSpace(val))
		if err != nil {
			return nil, fmt.Errorf("invalid BEAVEND_HTTP_IDLE_TIMEOUT: %w", err)
		}
		cfg.HTTP.IdleTimeout = d
	}

	if val := lookup("BEAVEND_DATABASE_URL"); val != "" {
		cfg.Database.URL = strings.TrimSpace(val)
	}
	if val := lookup("BEAVEND_REDIS_URL"); val != "" {
		cfg.Redis.URL = strings.TrimSpace(val)
	}
	if val := lookup("BEAVEND_LITELLM_URL"); val != "" {
		cfg.LiteLLM.URL = strings.TrimSpace(val)
	}
	if val := lookup("BEAVEND_LITELLM_KEY"); val != "" {
		cfg.LiteLLM.Key = strings.TrimSpace(val)
	}
	if val := lookup("BEAVEND_MASTER_KEY"); val != "" {
		cfg.Security.MasterKey = strings.TrimSpace(val)
	}
	if val := lookup("BEAVEND_ADMIN_EMAIL"); val != "" {
		cfg.Admin.Email = strings.TrimSpace(val)
	}
	if val := lookup("BEAVEND_ADMIN_PASSWORD"); val != "" {
		cfg.Admin.Password = strings.TrimSpace(val)
	}
	if val := lookup("BEAVEND_LOG_LEVEL"); val != "" {
		cfg.Log.Level = strings.TrimSpace(val)
	}
	if val := lookup("BEAVEND_LOG_FORMAT"); val != "" {
		cfg.Log.Format = strings.TrimSpace(val)
	}
	if val := lookup("BEAVEND_USAGE_BUFFER_SIZE"); val != "" {
		size, err := strconv.Atoi(strings.TrimSpace(val))
		if err != nil {
			return nil, fmt.Errorf("invalid BEAVEND_USAGE_BUFFER_SIZE: %w", err)
		}
		cfg.Usage.BufferSize = size
	}
	if val := lookup("BEAVEND_USAGE_BATCH_SIZE"); val != "" {
		size, err := strconv.Atoi(strings.TrimSpace(val))
		if err != nil {
			return nil, fmt.Errorf("invalid BEAVEND_USAGE_BATCH_SIZE: %w", err)
		}
		cfg.Usage.BatchSize = size
	}
	if val := lookup("BEAVEND_USAGE_FLUSH_INTERVAL"); val != "" {
		d, err := time.ParseDuration(strings.TrimSpace(val))
		if err != nil {
			return nil, fmt.Errorf("invalid BEAVEND_USAGE_FLUSH_INTERVAL: %w", err)
		}
		cfg.Usage.FlushInterval = d
	}
	if val := lookup("BEAVEND_USAGE_ROLLUP_INTERVAL"); val != "" {
		d, err := time.ParseDuration(strings.TrimSpace(val))
		if err != nil {
			return nil, fmt.Errorf("invalid BEAVEND_USAGE_ROLLUP_INTERVAL: %w", err)
		}
		cfg.Usage.RollupInterval = d
	}

	if val := lookup("BEAVEND_SEARXNG_URL"); val != "" {
		cfg.Search.SearxngURL = strings.TrimSpace(val)
	}
	if val := lookup("BEAVEND_SEARXNG_ENABLED"); val != "" {
		cfg.Search.Enabled = strings.EqualFold(strings.TrimSpace(val), "true") || strings.TrimSpace(val) == "1"
	}
	if val := lookup("BEAVEND_SEARXNG_TIMEOUT"); val != "" {
		if d, err := time.ParseDuration(strings.TrimSpace(val)); err == nil {
			cfg.Search.Timeout = d
		}
	}

	if val := lookup("BEAVEND_PRICING_AUTO_SYNC"); val != "" {
		cfg.Pricing.AutoSync = strings.EqualFold(strings.TrimSpace(val), "true") || strings.TrimSpace(val) == "1"
	}
	if val := lookup("BEAVEND_PRICING_CATALOG_URL"); val != "" {
		cfg.Pricing.CatalogURL = strings.TrimSpace(val)
	}
	if val := lookup("BEAVEND_PRICING_SYNC_INTERVAL"); val != "" {
		if d, err := time.ParseDuration(strings.TrimSpace(val)); err == nil {
			cfg.Pricing.SyncInterval = d
		}
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	switch c.Role {
	case RoleAll, RoleRuntime, RoleControl:
	default:
		return fmt.Errorf("invalid role %q: must be one of %q, %q, %q", c.Role, RoleAll, RoleRuntime, RoleControl)
	}

	switch c.Env {
	case EnvDevelopment, EnvStaging, EnvProduction:
	default:
		return fmt.Errorf("invalid environment %q: must be one of %q, %q, %q", c.Env, EnvDevelopment, EnvStaging, EnvProduction)
	}

	if c.HTTP.Port <= 0 || c.HTTP.Port > 65535 {
		return fmt.Errorf("invalid HTTP port: %d (must be 1-65535)", c.HTTP.Port)
	}

	if c.Database.URL == "" {
		return fmt.Errorf("database URL must not be empty")
	}
	if !strings.HasPrefix(c.Database.URL, "postgres://") && !strings.HasPrefix(c.Database.URL, "postgresql://") {
		return fmt.Errorf("malformed database connection string: must start with postgres:// or postgresql://")
	}

	if c.Redis.URL != "" && !strings.HasPrefix(c.Redis.URL, "redis://") && !strings.HasPrefix(c.Redis.URL, "rediss://") {
		return fmt.Errorf("malformed redis connection string: must start with redis:// or rediss://")
	}

	// Validate MasterKey: must be 32 bytes (64 hex characters)
	keyBytes, err := hex.DecodeString(c.Security.MasterKey)
	if err != nil || len(keyBytes) != 32 {
		return fmt.Errorf("security master key must be a valid 64-character (32 bytes) hex string")
	}

	if c.Env == EnvProduction {
		defaultKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		if strings.EqualFold(c.Security.MasterKey, defaultKey) {
			return fmt.Errorf("default master key is forbidden in production environment")
		}
		if c.Admin.Password == "adminpassword123" {
			return fmt.Errorf("default admin password is forbidden in production environment")
		}
	}

	if c.Usage.BufferSize <= 0 {
		return fmt.Errorf("invalid usage buffer size: %d (must be > 0)", c.Usage.BufferSize)
	}
	if c.Usage.BatchSize <= 0 {
		return fmt.Errorf("invalid usage batch size: %d (must be > 0)", c.Usage.BatchSize)
	}
	if c.Usage.FlushInterval <= 0 {
		return fmt.Errorf("invalid usage flush interval: %v (must be > 0)", c.Usage.FlushInterval)
	}
	if c.Usage.RollupInterval <= 0 {
		return fmt.Errorf("invalid usage rollup interval: %v (must be > 0)", c.Usage.RollupInterval)
	}

	return nil
}
