# T086 - Generator Interface & Media Generation Tools

* **Status:** Complete

## Goal
Support image/audio/video generation both via routing and model-driven tool calls.

## Scope
1. `core.Generator` optional interface and request/response types; implement in openaicompat (images, speech) and litellm adapters.
2. Built-in tools `generate_image`, `generate_speech`, `generate_video` each bound to a user-chosen logical model; use agent loop depth, confirmation and quota rules.
3. Async job flow for video: job store in Redis, polling endpoint, optional webhook.
4. Output guardrails on generated media; SSRF-safe fetching of remote media.
5. Usage events carry per-model units (images, seconds); pricing updates.

## Acceptance Criteria
- [x] `output_modalities: ["image"]` routes to the configured image model.
- [x] Chat model can call `generate_image` and receive a signed URL.
- [x] Fallbacks stay capability-compatible.
