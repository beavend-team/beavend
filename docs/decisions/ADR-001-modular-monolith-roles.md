# ADR-001: Modular Monolith with Runtime and Control Role Separation

## Status
Accepted

## Context
Beavend must serve high-throughput, low-latency AI requests from end users while simultaneously offering a management API and dashboard for administrators. The system must be simple to self-host for small teams (`docker compose up`) while scaling horizontally to handle millions of daily requests in large cloud environments.

We evaluated whether to divide Beavend into microservices, maintain a single undifferentiated monolith, or adopt a modular monolith capable of assuming distinct runtime roles.

## Decision
We chose a **modular monolith built in Go**, packaged as a single deployable binary and container image. The binary supports three operational roles via the `--role` command-line flag:
* `--role=runtime`: Serves public AI runtime traffic (chat, streaming, tool dispatch).
* `--role=control`: Serves management APIs, dashboard assets, and config management.
* `--role=all` (default): Runs both runtime and control planes within a single process.

Code boundaries between runtime, control, and shared domain packages are strictly enforced at compile time via Go package layout and `depguard` linting rules.

## Consequences
### Positive
* **Operational Simplicity:** Single build artifact, one Docker image, zero distributed tracing or service mesh overhead for self-hosters.
* **Flexible Scaling:** Small installations run `--role=all` with minimal memory footprint; large production deployments deploy dedicated, horizontally auto-scaled `--role=runtime` pods.
* **Fault Isolation:** A crash or heavy query load on the control plane does not degrade the runtime plane's ability to serve end-user AI traffic.

### Negative
* Requires discipline in Go package boundaries to prevent runtime from accidentally coupling to control plane code.
* Single codebase requires shared dependency management (`go.mod`).

## Alternatives Considered
* **Microservices Architecture:** Deemed premature and operationally prohibitive for self-hosters and small maintenance teams.
* **Undifferentiated Monolith:** Rejected because admin operations and streaming AI workloads have fundamentally different scaling profiles and availability requirements.
