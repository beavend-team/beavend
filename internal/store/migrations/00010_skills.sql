-- +goose Up
CREATE TABLE IF NOT EXISTS skills (
    id VARCHAR(64) PRIMARY KEY,
    project_id VARCHAR(64) NOT NULL,
    environment VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    source VARCHAR(512) NOT NULL DEFAULT 'manual',
    injection_mode VARCHAR(32) NOT NULL DEFAULT 'always',
    active_version INT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, environment, name),
    FOREIGN KEY (project_id, environment) REFERENCES environments(project_id, name) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_skills_project_env ON skills(project_id, environment);

CREATE TABLE IF NOT EXISTS skill_versions (
    id VARCHAR(64) PRIMARY KEY,
    skill_id VARCHAR(64) NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    project_id VARCHAR(64) NOT NULL,
    environment VARCHAR(64) NOT NULL,
    version INT NOT NULL,
    content TEXT NOT NULL,
    variables JSONB NOT NULL DEFAULT '[]'::jsonb,
    is_active BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (skill_id, version),
    FOREIGN KEY (project_id, environment) REFERENCES environments(project_id, name) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_skill_versions_project_env ON skill_versions(project_id, environment);

ALTER TABLE ai_app_versions ADD COLUMN IF NOT EXISTS skill_ids JSONB NOT NULL DEFAULT '[]'::jsonb;

-- +goose Down
ALTER TABLE ai_app_versions DROP COLUMN IF EXISTS skill_ids;
DROP TABLE IF EXISTS skill_versions;
DROP TABLE IF EXISTS skills;
