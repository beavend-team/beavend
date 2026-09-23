# Cortexbase Local Development Guide

This guide details how to configure, develop, test, and contribute to the Cortexbase Go server, control plane, and administrative dashboard.

---

## 1. Prerequisites & Tooling

Ensure the following tools are installed on your workstation:

| Tool | Minimum Version | Purpose |
|---|---|---|
| **Go** | `1.22+` | Backend server & CLI runtime |
| **Node.js** | `20+` (LTS) | React admin dashboard build tooling |
| **npm** / **pnpm** | Current LTS | Dashboard package management |
| **Docker & Compose** | `24+` / Compose v2 | Local Postgres, Valkey/Redis, and LiteLLM |
| **golangci-lint** | `v1.57+` | Go static analysis and linting |
| **govulncheck** | Latest | Go vulnerability database scanner |
| **sqlc** | `v1.25+` | Type-safe SQL code generation |
| **goose** | `v3.18+` | Database migration management |

---

## 2. Environment Setup

### 2.1 Clone Repository & Install Dependencies
```bash
# In the cortexbase project directory
cd cortexbase

# Install Go dependencies
go mod download

# Install Dashboard dependencies
cd dashboard
npm install
cd ..
```

### 2.2 Local Infrastructure via Docker Compose
Cortexbase provides a minimal `docker-compose.yml` for local development supporting PostgreSQL 16, Valkey (Redis-compatible), and LiteLLM:

```bash
# Start local databases and services
docker compose -f deploy/docker/docker-compose.yml up -d postgres valkey litellm
```

### 2.3 Environment Variables
Copy `.env.example` to `.env`:
```bash
# Server Configuration
CORTEXBASE_ENV=development
CORTEXBASE_ROLE=all
CORTEXBASE_HTTP_PORT=8080
CORTEXBASE_LOG_LEVEL=debug
CORTEXBASE_LOG_FORMAT=json

# Primary Storage (PostgreSQL)
CORTEXBASE_DATABASE_URL=postgres://cortexbase:cortexbase@localhost:5432/cortexbase?sslmode=disable

# Ephemeral State & Counters (Valkey / Redis)
CORTEXBASE_REDIS_URL=redis://localhost:6379/0

# Provider Gateway (LiteLLM)
CORTEXBASE_LITELLM_URL=http://localhost:4000
CORTEXBASE_LITELLM_KEY=sk-cortexbase-local-master

# Envelope Encryption (32-byte hex for AES-256)
CORTEXBASE_MASTER_KEY=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef

# Initial Admin Credentials (Control Plane)
CORTEXBASE_ADMIN_EMAIL=admin@cortexbase.local
CORTEXBASE_ADMIN_PASSWORD=adminpassword123
```

### 2.4 Apply Database Migrations
Run `goose` migrations against PostgreSQL:
```bash
goose -dir internal/store/migrations postgres "$CORTEXBASE_DATABASE_URL" up
```

---

## 3. Running the Server

### 3.1 All-In-One Mode (Default Local Dev)
Runs runtime plane, control plane, and embedded dashboard in one process:
```bash
go run ./cmd/cortexbase --role=all
```
* Runtime API: `http://localhost:8080/v1`
* Management API: `http://localhost:8080/v1/admin`
* Embedded Dashboard: `http://localhost:8080/`
* Health Check: `http://localhost:8080/healthz`
* Readiness Probe: `http://localhost:8080/readyz`

### 3.2 Running Independent Roles
To test multi-process separation:
```bash
# Terminal 1: Runtime plane on port 8081
CORTEXBASE_HTTP_PORT=8081 go run ./cmd/cortexbase --role=runtime

# Terminal 2: Control plane on port 8082
CORTEXBASE_HTTP_PORT=8082 go run ./cmd/cortexbase --role=control
```

### 3.3 Running Dashboard with Hot Reloading
For frontend development against a running control plane:
```bash
cd dashboard
VITE_API_URL=http://localhost:8080 npm run dev
```

---

## 4. Testing & Quality Assurance

### 4.1 Unit Tests
Unit tests run without external dependencies (in-memory mocks and CEL test vectors):
```bash
go test -race -v ./internal/...
```

### 4.2 Integration Tests
Integration tests spin up ephemeral PostgreSQL and Valkey containers via `testcontainers-go`:
```bash
go test -tags=integration -race -v ./...
```

### 4.3 Linting & Static Analysis
```bash
# Run golangci-lint
golangci-lint run ./...

# Run Go vulnerability check
govulncheck ./...

# Validate SQL queries and regenerate code
sqlc generate
```

---

## 5. Extending Cortexbase

### 5.1 How to Add a New LLM Provider Adapter
1. Navigate to `internal/providers/`.
2. Create a new package or file implementing `core.ProviderAdapter`:
   ```go
   type ProviderAdapter interface {
       Chat(ctx context.Context, req core.ChatRequest) (*core.ChatResponse, error)
       Stream(ctx context.Context, req core.ChatRequest) (<-chan core.StreamEvent, error)
       Embed(ctx context.Context, req core.EmbedRequest) (*core.EmbedResponse, error)
       Health(ctx context.Context) error
   }
   ```
3. Register the adapter in the provider factory `internal/providers/factory.go`.
4. Add unit tests verifying request/response conversion and error mapping to the Cortexbase error taxonomy.

### 5.2 How to Add a New Tool Executor
1. Navigate to `internal/tools/executors/`.
2. Implement the `core.Executor` interface:
   ```go
   type Executor interface {
       Execute(ctx context.Context, call core.ToolCall, id core.Identity) (core.ToolResult, error)
   }
   ```
3. If the executor connects over the network, ensure it enforces SSRF defenses using `internal/tools/ssrf.SafeDialer`.
4. Register the executor type in `internal/tools/engine.go`.

---

## 6. Pre-Submission Checklist

Before opening a pull request:
- [ ] `go fmt ./...` and `go vet ./...` produce zero diff.
- [ ] `golangci-lint run ./...` reports 0 issues.
- [ ] `govulncheck ./...` reports 0 vulnerabilities.
- [ ] All unit and integration tests pass: `go test -race ./...`.
- [ ] No database reads added to the runtime request hot-path.
- [ ] If API endpoints or parameters were modified, `api/openapi.yaml` was updated.
- [ ] If an architectural decision was made or changed, an ADR in `docs/decisions/` was added or revised.
