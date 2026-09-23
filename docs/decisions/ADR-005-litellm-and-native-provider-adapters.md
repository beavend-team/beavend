# ADR-005 — LiteLLM Gateway Default with Native OpenAI-Compatible Adapter

## Status
Accepted

## Context
Cortexbase must interface with numerous AI model providers (OpenAI, Anthropic, Google Gemini, Ollama, OpenRouter, Azure OpenAI, AWS Bedrock). Maintaining direct API integrations, SDKs, and error mappings for 50+ AI providers within Go would divert engineering focus away from application-level backend capabilities.

However, LiteLLM is written in Python. In high-concurrency environments, LiteLLM could become a throughput or latency bottleneck if forced into the path of all requests.

## Decision
We adopted a **hybrid provider architecture**:
1. All model interactions are abstracted behind the Go `ProviderAdapter` interface in `internal/core`.
2. **LiteLLM is the default implementation (`LiteLLMAdapter`)**, handling multi-provider breadth, automatic retries, provider fallback, and cost reporting. LiteLLM runs as a separate stateless service in Docker Compose or Kubernetes.
3. We provide a **native Go adapter (`OpenAICompatAdapter`)** from v0.2. This adapter speaks the OpenAI wire format directly from Go, bypassing LiteLLM for high-throughput deployments that utilize OpenAI, OpenRouter, Ollama, or OpenAI-compatible endpoints.
4. The adapter used for each logical model deployment is selectable via configuration.

## Consequences
### Positive
* Day-one support for 50+ providers without reinventing provider integrations.
* Zero vendor lock-in; LiteLLM is a swappable component, not a hard-coded runtime dependency.
* Provides a native Go scale escape-hatch capable of handling massive SSE streaming throughput directly.

### Negative
* Local development with Docker Compose requires running an additional container (`litellm`).
* Minor differences in usage and cost reporting between the native adapter and LiteLLM must be normalized.
