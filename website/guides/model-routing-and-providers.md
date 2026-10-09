# Model Routing & AI Providers

Beavend decouples client applications from physical AI providers. Client apps request **logical models** (such as `smart`, `fast`, or `code`), while Beavend resolves them dynamically to concrete deployments, balancing load, managing credentials, and cascading through fallbacks.

---

## 1. Provider Adapters

All LLM interactions pass through the `ProviderAdapter` domain interface ([`internal/core`](https://github.com/beavend/beavend/blob/master/internal/core)):

```go
type ProviderAdapter interface {
    Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
    Stream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error)
    Embed(ctx context.Context, req EmbedRequest) (*EmbedResponse, error)
    Health(ctx context.Context) error
}
```

Beavend ships with two battle-tested adapters:

### 1. LiteLLM Adapter (`litellm`)
* **Default Universal Gateway**: Mediates between Beavend and 100+ LLM APIs (OpenAI, Anthropic Claude, Google Gemini, AWS Bedrock, Azure OpenAI, Groq, Mistral, Ollama).
* Translates native provider schemas, manages upstream rate limit backoffs, and normalizes token usage metrics.

### 2. Native OpenAI-Compatible Adapter (`openai_compat`)
* **Direct High-Performance Adapter**: Directly invokes any OpenAI-compatible API without proxy overhead.
* Ideal for self-hosted instances running **vLLM**, **llama.cpp**, **LocalAI**, **Ollama**, or cloud providers like **DeepSeek**, **Together AI**, and **Groq**.

---

## 2. Logical Models & Deployments

Instead of hardcoding `gpt-4o` or `claude-3-5-sonnet` in mobile apps, define logical aliases in Beavend:

```mermaid
flowchart LR
    App["AI App Request<br/>model: 'smart'"] --> Resolver["Beavend Resolver"]
    Resolver -->|Primary (Priority 1)| D1["OpenAI: gpt-4o<br/>(weight: 80%)"]
    Resolver -->|Canary (Priority 1)| D2["Anthropic: claude-3-5-sonnet<br/>(weight: 20%)"]
    Resolver -.->|Fallback on 429/5xx (Priority 2)| D3["Google: gemini-1.5-pro"]
```

### Configuration Example (YAML / Config-as-Code):

```yaml
models:
  - id: "model-smart"
    name: "smart"
    description: "High-capability reasoning model"
    routes:
      - deployment: "openai-gpt4o"
        priority: 1
        weight: 80
      - deployment: "anthropic-claude"
        priority: 1
        weight: 20
      - deployment: "gemini-pro"
        priority: 2
        weight: 100  # Automatic fallback if Priority 1 fails

deployments:
  - id: "openai-gpt4o"
    provider: "litellm"
    model_name: "openai/gpt-4o"
    parameters:
      temperature: 0.7
      max_tokens: 4096

  - id: "gemini-pro"
    provider: "litellm"
    model_name: "gemini/gemini-1.5-pro"
    parameters:
      temperature: 0.7
      max_tokens: 4096
```

---

## 3. Automatic Failover & Resilience

When an upstream provider experiences downtime, returns HTTP 429 (rate limited), or times out:

1. The Resolver catches the error before emitting content to the user.
2. It logs a failover warning with telemetry attributes (`failed_deployment`, `error_status`).
3. It immediately invokes the next priority deployment in the route's fallback chain.
4. If all deployments are exhausted, Beavend returns a structured `503 service_unavailable` error with the error taxonomy code `UPSTREAM_UNAVAILABLE`.

---

## 4. Provider Credential Security

* **Envelope Encryption**: API keys for external providers are encrypted at rest in PostgreSQL using **AES-256-GCM** envelope encryption derived from `BEAVEND_MASTER_KEY`.
* **Zero Client Exposure**: Provider keys are never sent to client applications, never echoed in API responses, and automatically redacted from structured logs.
