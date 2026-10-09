# Configuration as Code & GitOps

Beavend supports **Configuration as Code (CaC)**, allowing engineering teams to export, version-control in Git, and promote full tenant configurations across environments (`development` → `staging` → `production`).

---

## 1. Why Configuration as Code?

* **Declarative GitOps**: Store all AI Apps, prompts, model routes, tool schemas, and CEL policies in your Git repository alongside application source code.
* **Auditability & Code Review**: Review changes to prompt templates, model routing weights, and authorization rules via standard Git Pull Requests.
* **Seamless Promotion**: Promote tested configurations between staging and production environments without manual clicking in the dashboard.
* **Zero-Secret Leakage**: Sensitive credentials (provider API keys, webhook secrets) are automatically redacted with cryptographic placeholders or re-encrypted using envelope encryption during export.

---

## 2. Using the CLI

The `beavend` binary includes built-in commands for managing declarative configuration:

### Exporting an Environment
Export the active configuration of a project to a declarative YAML file:

```bash
beavend config export \
  --project="my-project" \
  --env="staging" \
  --output="beavend-staging.yaml"
```

### Validating a Configuration File
Validate the syntax, JSON schemas, and CEL policies of a configuration file before deployment:

```bash
beavend config validate \
  --file="beavend-staging.yaml"
```

### Importing / Promoting to Production
Apply configuration changes to a target environment:

```bash
beavend config import \
  --project="my-project" \
  --env="production" \
  --file="beavend-staging.yaml" \
  --dry-run=false
```

---

## 3. Configuration File Structure

A Beavend configuration file captures the complete environment state in a clean, human-readable format:

```yaml
version: "1.0"
project:
  id: "proj-ecommerce"
  name: "E-Commerce Assistant"
  environment: "production"

auth:
  provider: "firebase"
  issuer: "https://securetoken.google.com/my-firebase-app"
  audience: "my-firebase-app"

models:
  - id: "model-smart"
    name: "smart"
    routes:
      - deployment: "openai-gpt4o"
        priority: 1
        weight: 100

policies:
  - id: "policy-require-login"
    name: "Require Valid Subject"
    expression: "identity.subject != ''"
    action: "allow"

apps:
  - id: "app-shopping-assistant"
    name: "Shopping Assistant"
    model: "smart"
    system_prompt: "You are an e-commerce assistant helping customers browse products."
    tools:
      - "get_order_status"
      - "search_catalog"
```

---

## 4. REST API Endpoints

You can also automate configuration synchronization directly through the Control Plane API in CI/CD pipelines:

* `GET /v1/admin/projects/{project_id}/export?env={env}`: Returns the exported configuration bundle.
* `POST /v1/admin/projects/{project_id}/import`: Applies a configuration bundle, bumps the project revision, and triggers instant in-memory snapshot compilation across the runtime fleet.
