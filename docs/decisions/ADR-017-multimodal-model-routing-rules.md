# ADR-017: Multimodal Model Routing Rules & Canonical Message Parts

## Status
Accepted

## Context
An AI App must be able to use several models for different purposes: accepting image/audio/video input, and producing image/audio/video output (e.g. a chat model delegating image generation to an image model). Today `routing.Resolver.selectLogicalModel` hardcodes the logical names `vision`, `reasoning` and `fast`, handles only image *input*, and has no notion of output modality. Implementors cannot name their own models freely nor decide what traffic goes where.

Additionally:
- `CacheStage` (ADR-011) runs before model routing, and `cache.ComputeRequestHash` does not include the routed model or output modality, so requests with different routes could collide.
- Conversation threads (ADR-012) must survive switching models mid-thread.

## Decision
1. **Beavend ships no reserved model names.** Logical model names are fully user-defined. Each logical model keeps its own deployment, adapter and fallbacks (existing `ModelRouting`).

2. **Per-app routing rules, evaluated in memory.** `AIApp` gains a `ModelRoutes` config:
   ```yaml
   model_routes:
     default: <logical model>        # required
     rules:                          # first match wins
       - when: { input_has: [image] }
         use: <logical model>
       - when: { output: [image] }
         use: <logical model>
       - when: { metadata: { complexity: high } }
         use: <logical model>
       - when: { expr: "..." }       # optional CEL (ADR-004)
         use: <logical model>
   ```
   - The `when` vocabulary (input modalities, requested output modalities, metadata, optional CEL) is the only thing Beavend defines.
   - Selection order: explicit request `model` (if allowed by policy) → rules → `default`. No implicit `fast` default; the legacy `ModelRoutingID` is migrated to `default`.
   - Compiled and validated in the snapshot compiler; invalid rules (unknown target, missing default) reject the publish.

3. **Capability metadata is for validation only.** `ModelRouting` gains optional `input_modalities` / `output_modalities`. They are NOT used for routing; they are used to (a) reject rules routing to an incapable model, (b) restrict fallbacks to equal-or-superset capabilities, (c) drive dashboard pickers.

4. **Requested output modality is explicit.** `ChatRequest.OutputModalities []string` (default `["text"]`). The server never infers it from prompt text.

5. **Model-driven delegation via tools.** Built-in tools (`generate_image`, `generate_speech`, `generate_video`) run through the existing tool engine and agent loop (bounded depth, confirmation, quota). Each tool targets a user-chosen logical model. Long-running generation (video) is asynchronous (job id + polling/webhook).

6. **Provider interface.** `ProviderAdapter.Chat/Stream` remain for text and multimodal input. A new optional capability interface `Generator` (`Generate(ctx, GenerateRequest) (*GenerateResponse, error)`) covers media generation, discovered via type assertion so existing adapters are unaffected.

7. **Canonical message parts.** Thread/message content is stored as `[]Part` (`text`, `media{media_id,mime,digest}`, `tool_call`, `tool_result`), never provider-native formats. Adapters translate at the boundary. Media is stored in object storage and referenced; generated media is returned as signed URLs, never inlined in SSE or cache. When the routed model lacks a modality used in history, the context builder substitutes a text placeholder/caption.

8. **Cache correctness.** Route *selection* (logical model name) is a pure in-memory function of the request and snapshot, so it moves **before** `CacheStage` (new `RouteSelectStage`); deployment/adapter resolution stays in `ModelRoutingStage`. The cache hash includes the selected logical model, `OutputModalities`, and attachment content digests (not URLs). Generated media responses are not cached unless the app opts in per modality.

9. **Limits, usage, safety.** Usage events and pricing record per-model units (tokens, images, audio/video seconds). Rate limits/CEL gain modality dimensions. Attachments are size/MIME-validated and fetched via the SSRF-safe dialer. Output guardrails apply to generated media.

## Consequences
- Implementors have full naming and topology freedom; behavior is declared, not implicit.
- The zero-DB hot-path invariant is preserved: rules compile into the snapshot.
- Breaking change for apps depending on the `vision`/`reasoning`/`fast` names → migration required.
- Pipeline order changes (ADR-011 amended): `RouteSelect` precedes `Cache`.
- Message storage format changes (canonical parts) → migration and adapter work.
- Added surface: object storage for media, async job handling.

## Alternatives Considered
- **Keep conventional names (`vision`, etc.):** simple but opinionated and not user-configurable.
- **Capability-based auto-routing (pick any model that supports the modality):** ambiguous when several match; takes away implementor control. Kept only as validation.
- **LLM-based router/classifier:** non-deterministic, adds latency and cost, hurts cache keys. Model-driven delegation is limited to the explicit tool path.
- **Overload `Chat` for media generation:** muddies the interface and streaming semantics; rejected for a separate `Generator` interface.
- **Pin a thread to one model:** breaks per-turn routing and fallbacks.
