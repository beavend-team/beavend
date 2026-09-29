# T044 — React + Vite Admin Dashboard Single Page Application

* **Status:** Completed

## Goal
Build a modern, responsive administrative Single Page Application (SPA) using React 18, TypeScript, Vite, and Tailwind CSS to manage Beavend instances.

## Depends On
- T041, T042, T043

## Scope
1. **Frontend Architecture (`dashboard/`):**
   - React 18 + TypeScript + Vite project.
   - Tailwind CSS for sleek, accessible UI styling.
   - Clean component hierarchy and shared UI components (Sidebar, Navbar, Cards, Badges, Modals, Forms, Tables).
2. **Dashboard Views & Navigation:**
   - **Login Page:** Clean authentication form supporting email/password and session persistence.
   - **Overview / Dashboard:**
     - High-level KPIs: Active Apps, Total Requests (24h), Token Usage, Estimated Cost, Error Rate.
     - Usage trend visualizer.
   - **AI Apps Management:**
     - List apps by project and environment.
     - Create & configure apps: assigned prompt, model routing, tools, guardrails, limits.
     - View version history.
   - **Prompts Studio:**
     - Template editor with variable detection (`{{variable}}`).
     - Test render preview.
     - Version list and active version selector.
   - **Models & Routings:**
     - Manage logical models (`fast`, `reasoning`, `vision`).
     - Configure LiteLLM / direct OpenAI-compatible deployments and fallbacks.
   - **Tools Registry:**
     - Tool editor: JSON Schema, identity bindings (`user_id -> claims.sub`), side effect mode (`read` / `write`), confirmation policy (`required` / `none`).
     - Downstream HTTP endpoint configuration and encrypted secret injection.
   - **Policies:**
     - CEL authorization rules editor with syntax validation feedback.
   - **Usage & Logs Explorer:**
     - Search and filter request metadata logs.
     - View latency, tokens, status codes, and tool execution traces.
   - **Settings & Audit:**
     - Project and environment selector / creator.
     - Admin users management (owner only).
     - Audit log history viewer.
3. **API Client & State Management:**
   - Typed API client communicating with `/v1/admin/*` endpoints.
   - Handles `401 Unauthorized` with automatic redirect to `/login`.
   - Passes `X-Project-ID` and `X-Environment` headers.
4. **Vite Production Build:**
   - Configured to output static bundle to `dashboard/dist/` with relative asset paths suitable for Go `embed.FS`.

## Acceptance Criteria
- [ ] React/Vite SPA builds cleanly without TypeScript or bundling errors.
- [ ] Production build generates static assets in `dashboard/dist`.
- [ ] UI provides comprehensive controls for all Beavend resources.
- [ ] Responsive, modern design with error states and loading skeletons.
