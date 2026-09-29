# T067 — Exporter, Importer Engine & Dry-Run Diff Engine

## Status
Completed

## Objective
Implement `internal/configcode/engine.go` providing complete Project & Environment export, YAML compilation, dry-run diffing against the PostgreSQL state, transactional import application, and snapshot invalidation.

## Key Changes
1. `Exporter`:
   - Queries `internal/store` for all project entities (Environment AuthConfig, AI Apps, Prompts, Models, Tools, Policies, Limits).
   - Masks secrets with `${ENV_VAR}` or safe placeholders.
   - Generates formatted, deterministic YAML document.
2. `Importer`:
   - Validates document syntax and `beavend_version`.
   - Computes `DiffReport`: resources created, updated, and unchanged.
   - In dry-run mode: returns `DiffReport` without mutating the database.
   - In apply mode: executes in a database transaction, updates or inserts records, preserves existing secrets for unchanged/masked fields, bumps project revision, and triggers snapshot syncer.
