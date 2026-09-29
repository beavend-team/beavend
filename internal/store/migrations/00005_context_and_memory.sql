-- +goose Up
CREATE EXTENSION IF NOT EXISTS vector;

-- Conversation threads
CREATE TABLE IF NOT EXISTS conversation_threads (
    id VARCHAR(64) NOT NULL,
    project_id VARCHAR(64) NOT NULL,
    environment VARCHAR(64) NOT NULL,
    ai_app_id VARCHAR(64) NOT NULL,
    issuer VARCHAR(255) NOT NULL DEFAULT '',
    subject VARCHAR(255) NOT NULL DEFAULT '',
    messages JSONB NOT NULL DEFAULT '[]'::jsonb,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    ttl_seconds INT NOT NULL DEFAULT 86400,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (project_id, environment, id),
    FOREIGN KEY (project_id, environment) REFERENCES environments(project_id, name) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_conversation_threads_app ON conversation_threads (project_id, environment, ai_app_id);
CREATE INDEX IF NOT EXISTS idx_conversation_threads_subject ON conversation_threads (project_id, environment, issuer, subject);
CREATE INDEX IF NOT EXISTS idx_conversation_threads_expires ON conversation_threads (expires_at);

-- Semantic memories
CREATE TABLE IF NOT EXISTS semantic_memories (
    id VARCHAR(64) NOT NULL,
    project_id VARCHAR(64) NOT NULL,
    environment VARCHAR(64) NOT NULL,
    ai_app_id VARCHAR(64) NOT NULL,
    issuer VARCHAR(255) NOT NULL,
    subject VARCHAR(255) NOT NULL,
    content TEXT NOT NULL,
    embedding vector,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ,
    PRIMARY KEY (project_id, environment, id),
    FOREIGN KEY (project_id, environment) REFERENCES environments(project_id, name) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_semantic_memories_subject ON semantic_memories (project_id, environment, issuer, subject, ai_app_id);
CREATE INDEX IF NOT EXISTS idx_semantic_memories_expires ON semantic_memories (expires_at);

-- Add memory_config to ai_app_versions
ALTER TABLE ai_app_versions ADD COLUMN IF NOT EXISTS memory_config JSONB NOT NULL DEFAULT '{}'::jsonb;

-- +goose Down
ALTER TABLE ai_app_versions DROP COLUMN IF EXISTS memory_config;
DROP TABLE IF EXISTS semantic_memories;
DROP TABLE IF EXISTS conversation_threads;
