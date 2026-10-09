# T058 - Dashboard AI App Details, Per-App Settings & Interactive Playground

* **Status:** Completed

## Goal
Transform the AI Apps section of the React/Vite dashboard from a flat list into a comprehensive App-centric workspace featuring an App Detail & Settings view with tabs for Model & Prompt, Tool Attachments, Rate Limits & Tiers, Guardrails & Safety, Response Caching, and an Interactive Playground.

## Scope
1. **App Workspace & Details Navigation (`dashboard/src/pages/AppsPage.tsx` or new subcomponents):**
   - Clickable App card opens the App workspace.
   - Breadcrumb navigation: `AI Applications / {appName}`.
2. **App Workspace Tabs:**
   - **Overview:**
     - App status, active version, creation date.
     - Endpoint URL (`POST /v1/apps/{id}/chat`).
     - Ready-to-use client code snippets (`curl`, Web/TypeScript, Kotlin, Swift).
   - **Configuration / Settings (Per-App):**
     - **Prompt & Routing:** Prompt template selector, prompt version, logical model selector (`fast`, `reasoning`).
     - **Tools:** Multi-select checklist of registered tools this bot can invoke.
     - **Rate Limiting & Tiers:**
       - Requests per minute / day limits.
       - Token quota (e.g. 100,000 / 24h).
       - CEL tier conditions editor (e.g. `when: claims.plan == 'pro'` -> 500 RPM, `default` -> 30 RPM).
     - **Guardrails & Safety:**
       - Toggles for PII redaction (Credit Card Luhn, SSN, Email, Phone, Secret Tokens).
       - Prompt injection defense toggle.
       - Max input size & max tokens bounds.
       - Streaming delivery mode (`buffered` vs `passthrough`).
     - **Response Caching:**
       - Cache enabled toggle, TTL (seconds/minutes).
       - Tool result caching toggle.
       - Cache scope (`user` vs `shared`).
     - **Access Policy & Schema:**
       - CEL access policy rule.
       - Response JSON Schema editor.
   - **Playground (Test Chat):**
     - Interactive chat interface to send user prompts to `/v1/apps/{id}/chat`.
     - Live inspection of response, tool calls, token usage, and latency.
3. **App Creation Modal:**
   - Improved creation dialog with optional initial rate limit and tool assignment.

## Acceptance Criteria
- [x] Users can click an AI App to view and edit all its per-app settings.
- [x] Saving settings updates the app in Beavend and invalidates the active snapshot.
- [x] Playground allows sending test messages directly from the dashboard.
- [x] Responsive design consistent with Tailwind dark mode palette.
