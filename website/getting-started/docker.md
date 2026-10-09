# Docker Build & Deployment Guide

This guide explains how to build the Beavend Docker container image from source, configure its environment variables, and run it locally or in staging environments.

> [!NOTE]
> Since this is an initial release, pre-built container images have not yet been published to a public registry (such as Docker Hub or GHCR). Follow the instructions below to build the container locally.

---

## 1. Multi-Stage Dockerfile Architecture

Beavend uses a three-stage build process defined in [`deploy/docker/Dockerfile`](https://github.com/beavend/beavend/blob/master/deploy/docker/Dockerfile) to produce a lean, secure, and self-contained container image:

```mermaid
flowchart TD
    subgraph Stage1["Stage 1: Frontend Builder (Node 20 Alpine)"]
        F_Src["dashboard/"] --> F_NPM["npm ci && npm run build"]
        F_NPM --> F_Dist["dashboard/dist/"]
    end

    subgraph Stage2["Stage 2: Go Compiler (Go 1.26 Alpine)"]
        G_Mod["go.mod / go.sum"] --> G_Deps["go mod download"]
        G_Src["cmd/ & internal/"] --> G_Build["CGO_ENABLED=0 go build"]
        F_Dist -.->|embed.FS| G_Build
        G_Build --> Binary["/beavend (static binary)"]
    end

    subgraph Stage3["Stage 3: Runtime Image (Alpine 3.20)"]
        Binary --> R_Img["/app/beavend"]
        CA["ca-certificates + tzdata"] --> R_Img
    end
```

### Key Advantages:
* **Zero Host Tooling Required**: You do not need Go or Node.js installed on your machine; Docker handles all compilation stages.
* **Embedded UI**: The compiled React dashboard SPA is embedded directly into the Go binary via Go's `embed.FS` mechanism.
* **Minimal Footprint**: The final runtime container contains only the Alpine base and the compiled binary (~45 MB), with no compilers or source code left behind.

---

## 2. Building the Docker Image

Run the build command from the **root directory** of the repository:

```bash
docker build -t beavend:latest -f deploy/docker/Dockerfile .
```

### Building for a Specific Platform (e.g. Cross-compilation)

If you are building on Apple Silicon (ARM64) and want to deploy to an AMD64 Linux server:

```bash
docker build --platform linux/amd64 -t beavend:latest -f deploy/docker/Dockerfile .
```

---

## 3. Running with Docker Compose

The simplest way to orchestrate Beavend with its PostgreSQL (with `pgvector`) and Valkey dependencies is via `docker-compose.yml`:

```bash
# Build and start the entire stack
docker compose -f deploy/docker/docker-compose.yml up --build -d
```

### Inspecting Container Logs
```bash
# Tail Beavend server logs
docker compose -f deploy/docker/docker-compose.yml logs -f beavend

# Tail PostgreSQL logs
docker compose -f deploy/docker/docker-compose.yml logs -f postgres
```

### Stopping the Stack
```bash
docker compose -f deploy/docker/docker-compose.yml down
```

---

## 4. Running a Standalone Container

If you already have existing PostgreSQL and Redis/Valkey instances running, you can run the Beavend container standalone:

```bash
docker run -d \
  --name beavend \
  -p 8080:8080 \
  -e BEAVEND_ENV=production \
  -e BEAVEND_ROLE=all \
  -e BEAVEND_HTTP_PORT=8080 \
  -e BEAVEND_DATABASE_URL="postgres://user:password@db.example.com:5432/beavend?sslmode=require" \
  -e BEAVEND_REDIS_URL="redis://valkey.example.com:6379/0" \
  -e BEAVEND_MASTER_KEY="0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" \
  -e BEAVEND_ADMIN_EMAIL="admin@yourcompany.com" \
  -e BEAVEND_ADMIN_PASSWORD="YourSecurePasswordHere!" \
  beavend:latest
```

---

## 5. Environment Variables Reference

| Variable | Default | Description |
|---|---|---|
| `BEAVEND_ROLE` | `all` | Operational plane role: `all`, `runtime`, or `control`. |
| `BEAVEND_ENV` | `development` | Environment name: `development`, `staging`, or `production`. |
| `BEAVEND_HTTP_PORT` | `8080` | TCP port for HTTP and SSE streaming ingress. |
| `BEAVEND_DATABASE_URL` | — | PostgreSQL connection string (must have `vector` extension available). |
| `BEAVEND_REDIS_URL` | — | Redis or Valkey connection string for pub/sub, caching, and rate limiting. |
| `BEAVEND_MASTER_KEY` | — | 64-character hex string (32 bytes) used for AES-256-GCM envelope encryption of stored secrets. |
| `BEAVEND_ADMIN_EMAIL` | `admin@beavend.local` | Initial administrative user email for the dashboard. |
| `BEAVEND_ADMIN_PASSWORD`| — | Initial password for the administrative user. |
| `BEAVEND_SEARXNG_URL` | — | Optional base URL for the SearXNG web search tool executor. |
| `BEAVEND_LOG_LEVEL` | `info` | Logging verbosity: `debug`, `info`, `warn`, `error`. |
| `BEAVEND_LOG_FORMAT` | `json` | Structured log output format: `json` or `text`. |

---

## 6. Role Splitting for Scalability

Beavend's single binary can be scaled independently across two distinct planes:

* **All-in-One (`BEAVEND_ROLE=all`)**: Runs both runtime and control planes in a single process. Recommended for local development and single-node installations.
* **Runtime Plane (`BEAVEND_ROLE=runtime`)**: Mounts only the public AI request endpoints (`/v1/apps/...`), the in-memory snapshot cache, and streaming pipelines. Stateless and horizontally autoscalable.
* **Control Plane (`BEAVEND_ROLE=control`)**: Mounts only administrative endpoints (`/v1/admin/...`), the embedded UI dashboard, and Postgres database writers. Can be kept in a private network or behind corporate VPN/SSO.
