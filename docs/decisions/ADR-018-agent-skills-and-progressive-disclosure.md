# ADR-018: Agent Skills & Progressive Disclosure

## Status
Accepted

## Context
Developers building AI Apps on Beavend need to attach modular, reusable instructions and domain workflows (known as **Agent Skills**, popularized by `SKILL.md` conventions and [skills.sh](https://skills.sh)) without duplicating monolithic system prompts across applications.

Furthermore:
- Some skills are foundational to every turn (e.g., brand voice, formatting rules) and should be injected directly into the system prompt (`always` mode) with zero extra LLM round-trips.
- Other skills are large, specialized procedural playbooks (e.g., SQL debugging, refund troubleshooting, API migration guides) where injecting all of them on every request would bloat prompt token costs. These require **Progressive Disclosure** (`on_demand` mode), where only lightweight catalog metadata (`id`, `name`, `description`) is included in the system prompt and the LLM can invoke a built-in `load_skill` tool when needed.
- `CacheStage` (ADR-011) runs before `PromptStage`, so attaching or detaching skills on an AI App must deterministically invalidate response caches.
- Importing skills from external registries (`skills.sh`, GitHub repositories, raw URLs) in the Control Plane must uphold Beavend's SSRF prevention boundaries.

## Decision
1. **First-Class Versioned Skills Registry (`skills` & `skill_versions`):**
   - Skills are scoped to `(project_id, environment)` and versioned immutably like prompts and tools.
   - Each skill defines `injection_mode` (`always` or `on_demand`), `source` (`manual` or URL/repo reference), `description`, and Markdown `content` with `{{variable}}` template interpolation support.
   - AI Apps reference attached skills via `skill_ids` (`JSONB` array of skill IDs on `ai_app_versions` and `SkillIDs []string` on `core.AIApp`).

2. **Zero-DB Hot Path via In-Memory Snapshot Compilation:**
   - Active skill versions are compiled into `core.Snapshot.Skills` (indexed by both skill ID and name) during snapshot compilation.
   - The snapshot compiler verifies referential integrity for all `app.SkillIDs` when `SkillQuerySource` is available.

3. **Dual Delivery Modes (`always` vs `on_demand`):**
   - **`always` Mode (System Prompt Injection):** During `PromptStage`, active `always` skills attached to the app have their `{{variables}}` interpolated using the request's variable context (`user`, `claims`, `app`, `project`, `metadata`) and are appended to the system prompt inside `<active_skills><skill id="..." name="...">...</skill></active_skills>`.
   - **`on_demand` Mode (Progressive Disclosure via `load_skill`):** During `PromptStage`, `on_demand` skills have only their `id`, `name`, and `description` listed inside `<available_skills>` in the system prompt. The bounded agent loop (`internal/runtime/agent`) automatically registers a built-in read-only `load_skill` tool (`Executor.Type: "skill_loader"`) constrained by an enum of the app's allowed on-demand skill IDs.
   - Because `SKILL.md` content is authored/imported by project administrators in the Control Plane and stored in the trusted `Snapshot`, `skill_loader` output is treated as trusted configuration (capped at 128KB, bypassing untrusted external tool prompt-injection regex filters that would false-positive on instructional Markdown).

4. **System Prompt Composition & Memory Preservation:**
   - `PromptStage` composes the base prompt template (or client pass-through system message when `PromptID == ""`), any vector memory recall system message prepended by `ContextStage`, and the rendered skills block without overwriting existing system context.

5. **Response Cache Key Canonicalization:**
   - `cache.ComputeRequestHash` includes the sorted `app.SkillIDs` slice in the canonical hash payload so attaching, detaching, or reordering skills immediately invalidates cached responses.

6. **SSRF-Safe `skills.sh` & GitHub Importer:**
   - The Control Plane exposes `POST /v1/admin/skills/import`, accepting `skills.sh` URLs, CLI commands (`npx skills add owner/repo --skill name`), GitHub shorthand (`owner/repo/skill`), GitHub blob URLs, or raw `SKILL.md` URLs.
   - All remote fetches use `tools.NewSafeHTTPClient` (enforcing DNS-rebinding-safe private IP/loopback/metadata denial) and a strict 512KB response size cap, parsing YAML frontmatter (`name`, `description`, `injection_mode`) automatically.

## Consequences
- Reusable skills can be shared across multiple AI Apps within a project and exported/imported declaratively via Config-as-Code (`ProjectDocument.Skills`).
- Zero-DB hot-path invariant is preserved: both `PromptStage` and `load_skill` read exclusively from `core.Snapshot` in memory.
- Progressive disclosure (`on_demand`) allows apps to attach dozens of specialized skills with minimal baseline token overhead.

## Alternatives Considered
- **Store skills only as inline text inside Prompts:** Prevents reuse across multiple AI Apps and does not support progressive disclosure (`on_demand` tool loading).
- **Fetch `skills.sh` content at runtime:** Violates the zero-dependency, low-latency hot-path design and introduces runtime availability risks. Importing at control-plane configuration time into versioned PostgreSQL rows + memory snapshots is deterministic and fast.
