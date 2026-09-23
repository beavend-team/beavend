-- +goose Up
-- Enable uuid extension if available
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Projects table
CREATE TABLE IF NOT EXISTS projects (
    id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Environments table
CREATE TABLE IF NOT EXISTS environments (
    project_id VARCHAR(64) NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name VARCHAR(64) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (project_id, name)
);

-- Project Revisions (used for snapshot cache invalidation)
CREATE TABLE IF NOT EXISTS project_revisions (
    project_id VARCHAR(64) NOT NULL,
    environment VARCHAR(64) NOT NULL,
    revision BIGINT NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (project_id, environment),
    FOREIGN KEY (project_id, environment) REFERENCES environments(project_id, name) ON DELETE CASCADE
);

-- Admin Users (control plane)
CREATE TABLE IF NOT EXISTS admin_users (
    id VARCHAR(64) PRIMARY KEY,
    email VARCHAR(255) NOT NULL UNIQUE,
    password_hash VARCHAR(255) NOT NULL,
    role VARCHAR(64) NOT NULL DEFAULT 'viewer',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Audit Log (control plane)
CREATE TABLE IF NOT EXISTS audit_log (
    id BIGSERIAL PRIMARY KEY,
    project_id VARCHAR(64) NOT NULL,
    environment VARCHAR(64) NOT NULL,
    user_id VARCHAR(64) NOT NULL,
    action VARCHAR(128) NOT NULL,
    resource_type VARCHAR(64) NOT NULL,
    resource_id VARCHAR(64) NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_audit_log_project_env ON audit_log(project_id, environment);
CREATE INDEX IF NOT EXISTS idx_audit_log_created_at ON audit_log(created_at);

-- Prompts
CREATE TABLE IF NOT EXISTS prompts (
    id VARCHAR(64) PRIMARY KEY,
    project_id VARCHAR(64) NOT NULL,
    environment VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    active_version INT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, environment, name),
    FOREIGN KEY (project_id, environment) REFERENCES environments(project_id, name) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_prompts_project_env ON prompts(project_id, environment);

-- Prompt Versions (immutable)
CREATE TABLE IF NOT EXISTS prompt_versions (
    id VARCHAR(64) PRIMARY KEY,
    prompt_id VARCHAR(64) NOT NULL REFERENCES prompts(id) ON DELETE CASCADE,
    project_id VARCHAR(64) NOT NULL,
    environment VARCHAR(64) NOT NULL,
    version INT NOT NULL,
    template TEXT NOT NULL,
    variables JSONB NOT NULL DEFAULT '[]'::jsonb,
    is_active BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (prompt_id, version),
    FOREIGN KEY (project_id, environment) REFERENCES environments(project_id, name) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_prompt_versions_project_env ON prompt_versions(project_id, environment);

-- Policies
CREATE TABLE IF NOT EXISTS policies (
    id VARCHAR(64) PRIMARY KEY,
    project_id VARCHAR(64) NOT NULL,
    environment VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    active_version INT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, environment, name),
    FOREIGN KEY (project_id, environment) REFERENCES environments(project_id, name) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_policies_project_env ON policies(project_id, environment);

-- Policy Versions (immutable)
CREATE TABLE IF NOT EXISTS policy_versions (
    id VARCHAR(64) PRIMARY KEY,
    policy_id VARCHAR(64) NOT NULL REFERENCES policies(id) ON DELETE CASCADE,
    project_id VARCHAR(64) NOT NULL,
    environment VARCHAR(64) NOT NULL,
    version INT NOT NULL,
    expression TEXT NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (policy_id, version),
    FOREIGN KEY (project_id, environment) REFERENCES environments(project_id, name) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_policy_versions_project_env ON policy_versions(project_id, environment);

-- Tools
CREATE TABLE IF NOT EXISTS tools (
    id VARCHAR(64) PRIMARY KEY,
    project_id VARCHAR(64) NOT NULL,
    environment VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    active_version INT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, environment, name),
    FOREIGN KEY (project_id, environment) REFERENCES environments(project_id, name) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_tools_project_env ON tools(project_id, environment);

-- Tool Versions (immutable)
CREATE TABLE IF NOT EXISTS tool_versions (
    id VARCHAR(64) PRIMARY KEY,
    tool_id VARCHAR(64) NOT NULL REFERENCES tools(id) ON DELETE CASCADE,
    project_id VARCHAR(64) NOT NULL,
    environment VARCHAR(64) NOT NULL,
    version INT NOT NULL,
    side_effect VARCHAR(32) NOT NULL DEFAULT 'read',
    input_schema JSONB NOT NULL DEFAULT '{}'::jsonb,
    identity_bindings JSONB NOT NULL DEFAULT '{}'::jsonb,
    executor JSONB NOT NULL DEFAULT '{}'::jsonb,
    downstream_auth JSONB NOT NULL DEFAULT '{}'::jsonb,
    confirmation VARCHAR(32) NOT NULL DEFAULT 'none',
    "authorization" TEXT NOT NULL DEFAULT '',
    result_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    cache_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    is_active BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tool_id, version),
    FOREIGN KEY (project_id, environment) REFERENCES environments(project_id, name) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_tool_versions_project_env ON tool_versions(project_id, environment);

-- Tool Secrets (encrypted)
CREATE TABLE IF NOT EXISTS tool_secrets (
    id VARCHAR(64) PRIMARY KEY,
    project_id VARCHAR(64) NOT NULL,
    environment VARCHAR(64) NOT NULL,
    tool_id VARCHAR(64) NOT NULL REFERENCES tools(id) ON DELETE CASCADE,
    secret_ref VARCHAR(128) NOT NULL,
    encrypted_secret BYTEA NOT NULL,
    nonce BYTEA NOT NULL,
    key_id VARCHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, environment, tool_id, secret_ref),
    FOREIGN KEY (project_id, environment) REFERENCES environments(project_id, name) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_tool_secrets_project_env ON tool_secrets(project_id, environment);

-- Provider Configs (encrypted credentials)
CREATE TABLE IF NOT EXISTS provider_configs (
    id VARCHAR(64) PRIMARY KEY,
    project_id VARCHAR(64) NOT NULL,
    environment VARCHAR(64) NOT NULL,
    provider_name VARCHAR(64) NOT NULL,
    base_url TEXT NOT NULL DEFAULT '',
    encrypted_api_key BYTEA NOT NULL,
    nonce BYTEA NOT NULL,
    key_id VARCHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, environment, provider_name),
    FOREIGN KEY (project_id, environment) REFERENCES environments(project_id, name) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_provider_configs_project_env ON provider_configs(project_id, environment);

-- Model Routings (logical models to provider deployments)
CREATE TABLE IF NOT EXISTS model_routings (
    id VARCHAR(64) PRIMARY KEY,
    project_id VARCHAR(64) NOT NULL,
    environment VARCHAR(64) NOT NULL,
    logical_model VARCHAR(128) NOT NULL,
    deployment VARCHAR(255) NOT NULL,
    adapter VARCHAR(64) NOT NULL DEFAULT 'litellm',
    fallback_deployments JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, environment, logical_model),
    FOREIGN KEY (project_id, environment) REFERENCES environments(project_id, name) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_model_routings_project_env ON model_routings(project_id, environment);

-- Guardrails
CREATE TABLE IF NOT EXISTS guardrails (
    id VARCHAR(64) PRIMARY KEY,
    project_id VARCHAR(64) NOT NULL,
    environment VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    active_version INT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, environment, name),
    FOREIGN KEY (project_id, environment) REFERENCES environments(project_id, name) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_guardrails_project_env ON guardrails(project_id, environment);

-- Guardrail Versions (immutable)
CREATE TABLE IF NOT EXISTS guardrail_versions (
    id VARCHAR(64) PRIMARY KEY,
    guardrail_id VARCHAR(64) NOT NULL REFERENCES guardrails(id) ON DELETE CASCADE,
    project_id VARCHAR(64) NOT NULL,
    environment VARCHAR(64) NOT NULL,
    version INT NOT NULL,
    stream_mode VARCHAR(64) NOT NULL DEFAULT 'buffered',
    max_input_bytes INT NOT NULL DEFAULT 65536,
    max_output_bytes INT NOT NULL DEFAULT 131072,
    pii_redaction BOOLEAN NOT NULL DEFAULT FALSE,
    injection_defense BOOLEAN NOT NULL DEFAULT FALSE,
    rules JSONB NOT NULL DEFAULT '[]'::jsonb,
    is_active BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (guardrail_id, version),
    FOREIGN KEY (project_id, environment) REFERENCES environments(project_id, name) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_guardrail_versions_project_env ON guardrail_versions(project_id, environment);

-- AI Apps
CREATE TABLE IF NOT EXISTS ai_apps (
    id VARCHAR(64) PRIMARY KEY,
    project_id VARCHAR(64) NOT NULL,
    environment VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    active_version INT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, environment, name),
    FOREIGN KEY (project_id, environment) REFERENCES environments(project_id, name) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_ai_apps_project_env ON ai_apps(project_id, environment);

-- AI App Versions
CREATE TABLE IF NOT EXISTS ai_app_versions (
    id VARCHAR(64) PRIMARY KEY,
    app_id VARCHAR(64) NOT NULL REFERENCES ai_apps(id) ON DELETE CASCADE,
    project_id VARCHAR(64) NOT NULL,
    environment VARCHAR(64) NOT NULL,
    version INT NOT NULL,
    access_policy TEXT NOT NULL DEFAULT '',
    prompt_id VARCHAR(64) NOT NULL DEFAULT '',
    prompt_version INT NOT NULL DEFAULT 1,
    model_routing_id VARCHAR(64) NOT NULL DEFAULT '',
    tool_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    limits JSONB NOT NULL DEFAULT '[]'::jsonb,
    cache_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    guardrails JSONB NOT NULL DEFAULT '{}'::jsonb,
    response_schema TEXT NOT NULL DEFAULT '',
    is_active BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (app_id, version),
    FOREIGN KEY (project_id, environment) REFERENCES environments(project_id, name) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_ai_app_versions_project_env ON ai_app_versions(project_id, environment);

-- +goose Down
DROP TABLE IF EXISTS ai_app_versions;
DROP TABLE IF EXISTS ai_apps;
DROP TABLE IF EXISTS guardrail_versions;
DROP TABLE IF EXISTS guardrails;
DROP TABLE IF EXISTS model_routings;
DROP TABLE IF EXISTS provider_configs;
DROP TABLE IF EXISTS tool_secrets;
DROP TABLE IF EXISTS tool_versions;
DROP TABLE IF EXISTS tools;
DROP TABLE IF EXISTS policy_versions;
DROP TABLE IF EXISTS policies;
DROP TABLE IF EXISTS prompt_versions;
DROP TABLE IF EXISTS prompts;
DROP TABLE IF EXISTS audit_log;
DROP TABLE IF EXISTS admin_users;
DROP TABLE IF EXISTS project_revisions;
DROP TABLE IF EXISTS environments;
DROP TABLE IF EXISTS projects;
