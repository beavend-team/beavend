# ADR-013 — Configuration as Code (Export, Import & Environment Promotion)

## Status
Accepted

## Context
As teams scale their adoption of Beavend across development, staging, and production environments, managing AI Apps, Prompts, Models, Tools, Policies, Limits, and Guardrails exclusively through a web dashboard becomes insufficient. Enterprise software engineering teams require:
1. **Declarative GitOps Workflows:** Ability to export complete project configurations into version-controlled YAML files tracked in git repositories.
2. **Environment Promotion:** Promoting vetted prompts, model routing configs, and tools from `development` into `staging` and `production` with auditability and minimal drift.
3. **Secret Reference Protection:** AI provider API keys (OpenAI, Gemini, LiteLLM) and downstream tool authentication tokens must never be exported in plain text to disk or git repositories.
4. **Dry-Run Diff Inspection:** Computing and reviewing exact create, update, and unchanged operations before mutating production database state.
5. **Atomic Runtime Invalidation:** Applying configuration changes transactionally and notifying all runtime instances via Redis pub/sub to reload memory snapshots without downtime.

In accordance with PRD §31 (Configuration as Code), PRD §36 (Roadmap v0.1), and Architecture §18, Beavend requires a complete Configuration as Code engine.

## Decision
1. **Schema & Versioning (`internal/configcode`):**
   - Declare `ProjectDocument` tagged with `beavend_version: "0.1"`.
   - Comprehensive declarative representation encompassing:
     - Project metadata (`id`, `name`, `description`, `allowed_origins`, `allowed_bundles`).
     - Environment name and `AuthConfig` (Firebase, Supabase, Generic OIDC).
     - AI Apps (prompts, model routings, attached tools, limits, caching, guardrails, memory, response schemas).
     - Prompts (templates, variables, active versions).
     - Models & Model Routings (logical models, deployments, fallback deployments, provider configurations).
     - Tools (HTTP, webhook, client-side executors, input schemas, identity bindings, confirmation, downstream auth, caching).
     - Policies (CEL authorization rules).

2. **Secret Masking & Environment Variable Resolution:**
   - **On Export:** Sensitive secrets (e.g. LiteLLM master keys, tool downstream auth tokens) are masked using safe placeholders (`[SECRET_MASKED]`) or environment variable references (`${VAR_NAME}`). Plaintext keys are never emitted.
   - **On Import:**
     - Values matching `${VAR_NAME}` or `env:VAR_NAME` are resolved from the server's OS environment variables.
     - When a secret is masked with `[SECRET_MASKED]` and the target entity already exists in PostgreSQL, its existing encrypted secret in the database is preserved without overwriting.
     - New raw secrets are encrypted using AES-256-GCM envelope encryption with the project master key.

3. **Dry-Run Diff Plan Engine:**
   - The `Engine.Diff` method compares incoming document specifications against current PostgreSQL entity records.
   - Generates a granular `DiffReport`:
     - Per-resource actions (`create`, `update`, `unchanged`).
     - Detailed field change list (e.g., `prompt_id: "p1" -> "p2"`, `template changed`, `deployment: "gpt-4o-mini" -> "gpt-4o"`).
     - Aggregate summary counters (`total_created`, `total_updated`, `total_unchanged`).

4. **Transactional Apply & Snapshot Invalidation:**
   - The `Engine.Apply` method creates or updates database records, increments the `project_revisions` sequence, and publishes a revision event via `synckit.Publisher` (Redis pub/sub with PostgreSQL LISTEN/NOTIFY fallback).
   - All runtime nodes receive the revision signal and atomically reload their compiled in-memory snapshot (`atomic.Pointer[*Snapshot]`) without querying PostgreSQL on the request hot path.

5. **CLI Integration (`cmd/beavend`):**
   - Subcommand `beavend export --project=<id> --env=<env> [--output=<file>]`: Connects to database and exports clean YAML.
   - Subcommand `beavend import --project=<id> --env=<env> --file=<file> [--dry-run]`: Parses YAML, displays color-coded diff plans, and applies updates directly.

6. **Control Plane REST API & Dashboard UI:**
   - `GET /v1/admin/projects/{project_id}/environments/{name}/export`: Downloads YAML configuration with `Content-Disposition: attachment`.
   - `POST /v1/admin/projects/{project_id}/environments/{name}/import`: Accepts YAML or JSON, supports `?dry_run=true`, and emits synchronous audit log events.
   - Dedicated "Config as Code" panel in Dashboard Settings with one-click export, YAML syntax editor/uploader, and visual diff preview.

## Consequences
### Positive
* Complete GitOps lifecycle for Beavend configurations; projects can be versioned, reviewed in pull requests, and deployed via CI/CD pipelines.
* Safe multi-environment promotion prevents accidental configuration drift between staging and production.
* Secret masking guarantees credentials never leak into git repositories or exported backup files.
* Runtime zero-DB hot-path invariant is strictly maintained via instant Redis snapshot invalidation.

### Negative
* Reconciling relational versions requires careful handling of foreign-key dependencies (e.g., creating prompts and tools before referencing them in AI Apps).

## Alternatives Considered
* **Database Dump/Restore:** Using pg_dump/pg_restore was rejected because it leaks secrets, breaks tenant isolation, and does not allow human-readable Git diffs or selective environment promotion.
* **Terraform Provider Only:** Requiring an external Terraform provider before native YAML support was rejected in favor of zero-dependency CLI/API commands that work out of the box.
