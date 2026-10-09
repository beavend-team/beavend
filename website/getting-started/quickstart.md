# Quickstart Guide

Get Beavend running locally in under five minutes with Docker Compose or from source.

---

## Prerequisites

* **Docker** (version 24.0+ recommended) and **Docker Compose** v2.
* *(Optional for source builds)*: **Go 1.26+** and **Node.js 20+**.

---

## 1. Start the Local Stack

The repository includes a ready-to-run Docker Compose configuration located at `deploy/docker/docker-compose.yml`. It provisions:

1. **PostgreSQL 16 with pgvector**: For durable project configuration, audit logs, usage tracking, and vector memory.
2. **Valkey 7.2 (Redis-compatible)**: For in-memory rate limiting, response caching, pub/sub revision events, and suspended tool confirmation tokens.
3. **SearXNG**: Privacy-respecting metasearch engine used by the web search tool executor.
4. **Beavend**: The unified binary running in all-in-one mode (`--role=all`) with the embedded dashboard SPA.

Clone the repository and launch the stack:

```bash
git clone https://github.com/beavend/beavend.git
cd beavend

# Start all services in the background
docker compose -f deploy/docker/docker-compose.yml up -d
```

Check service status:

```bash
docker compose -f deploy/docker/docker-compose.yml ps
```

---

## 2. Access the Admin Dashboard

Once the containers are healthy, open your browser:

* **Dashboard URL**: [http://localhost:8080/](http://localhost:8080/)
* **Default Admin Email**: `admin@beavend.local`
* **Default Admin Password**: `adminpassword123`

*(These defaults are configured in `deploy/docker/docker-compose.yml` for local development. Always customize them in production via `BEAVEND_ADMIN_EMAIL` and `BEAVEND_ADMIN_PASSWORD`.)*

---

## 3. Verify Health Probes

Beavend exposes unauthenticated probe endpoints:

```bash
# Liveness probe
curl -i http://localhost:8080/healthz

# Readiness probe (verifies database connectivity and snapshot readiness)
curl -i http://localhost:8080/readyz
```

Expected response:
```json
{"status":"ok"}
```

---

## 4. Run from Source (Alternative)

If you prefer to run Beavend directly on your host machine:

### Step 1: Start Supporting Infrastructure
```bash
docker compose -f deploy/docker/docker-compose.yml up -d postgres valkey searxng
```

### Step 2: Build the Admin Dashboard SPA
```bash
cd dashboard
npm install
npm run build
cd ..
```

### Step 3: Run the Beavend Server
```bash
# Load development environment variables
export $(cat .env.example | xargs)

# Run the single binary in all-in-one mode
go run ./cmd/beavend --role=all
```

---

## 5. Send Your First Runtime Request

After configuring a project, an AI App (e.g. ID `app-demo`), and a model route in the dashboard:

```bash
curl -X POST http://localhost:8080/v1/apps/app-demo/chat \
  -H "Authorization: Bearer <USER_JWT_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [
      {
        "role": "user",
        "content": "Hello Beavend! What tools are available?"
      }
    ]
  }'
```

To stream responses in real-time via Server-Sent Events (SSE):

```bash
curl -N -X POST http://localhost:8080/v1/apps/app-demo/stream \
  -H "Authorization: Bearer <USER_JWT_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [
      {
        "role": "user",
        "content": "Summarize this request in real-time."
      }
    ]
  }'
```

---

## Next Steps

* [Learn how to build the Docker image](docker.md)
* [Configure your Identity Provider (Firebase / Supabase / OIDC)](../guides/auth-and-identity.md)
* [Setup Model Routing and AI Providers](../guides/model-routing-and-providers.md)
* [Understand Beavend's CEL Policy Engine](../guides/cel-policies.md)
