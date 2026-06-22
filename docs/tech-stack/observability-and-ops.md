# Observability and Operations Stack

## Principle
Observability is a product requirement (see [FR-11/FR-13 equivalents in the master PRD](../prd/00-master-prd.md)), not an internal nice-to-have — SLO attainment is published on a public status page. The constraint, given [ADR-0002](../decisions/0002-hosting-lightweight-containers.md), is achieving this *without* self-hosting a heavy monitoring stack.

## Instrumentation
- **OpenTelemetry** across all Go services: metrics, traces, and structured logs share trace/span IDs so an ingestion → event → API request path can be followed end-to-end.
- Every service exposes a Prometheus-compatible `/metrics` endpoint (Go's `promhttp` handler) regardless of where metrics ultimately get shipped, so local/self-hosted setups can scrape it directly too.

## Where metrics/logs/traces live
**Recommended default: Grafana Cloud free tier.** It includes hosted Prometheus-compatible metrics (via `remote_write`), Loki for logs, Tempo for traces, dashboards, and basic alerting (Grafana OnCall) — all within Grafana Cloud's free usage allotment at Sky Radar's scale. This avoids self-hosting and operating Prometheus + Grafana + Loki + Tempo as four more long-running services on volunteer-maintained infra, which would directly contradict [ADR-0002](../decisions/0002-hosting-lightweight-containers.md)'s rationale.

**Self-host alternative (lite/local mode):** a `docker-compose` profile runs Prometheus + Grafana locally for development and for anyone self-hosting Sky Radar who'd rather not depend on a third-party SaaS. This is the same compose stack used for local dev (see [`hosting-and-deployment.md`](hosting-and-deployment.md)), so it's not extra work to maintain — it's the default dev environment, just also usable in production by self-hosters.

## Dashboards and alerting
- **SLO dashboard:** one dashboard per SLO in the [master PRD](../prd/00-master-prd.md#8-non-functional-requirements-slos) (availability, freshness, event latency, API latency, cache hit rate, cost) — this dashboard's public read-only view *is* the public status page (see [phase-4-packaging-and-governance.md](../prd/phase-4-packaging-and-governance.md)).
- **Alerting is burn-rate based, not raw-threshold based**, e.g., alert when the error budget for monthly availability is being consumed fast enough to exhaust it before month-end, not just "error rate > X for 5 minutes" — this avoids noisy alerts on brief blips while still catching real degradation early.
- **Every alert links to a runbook entry.** An alert with no corresponding runbook is treated as a defect in the alert, not shipped.
- **Cost alerting:** a budget-burn alert (cloud billing API or PaaS usage API, depending on what's available) fires before the declared monthly cap in [`hosting-and-deployment.md`](hosting-and-deployment.md) is reached.

## On-call (volunteer model)
- Lightweight rotation among maintainers, routed via Grafana OnCall (included in the free tier) or an equivalent free-tier-compatible tool.
- Any SLO-impacting incident gets a public, blameless postmortem published in the repo (`docs/postmortems/`, created in [Phase 4](../prd/phase-4-packaging-and-governance.md)) — both an operational practice and a content asset demonstrating real production maturity.

## CI quality gates (also "operations," enforced before code ships)
- Lint (`golangci-lint`, frontend ESLint/TypeScript checks).
- Unit + integration tests (see [`backend.md`](backend.md) and [`frontend.md`](frontend.md)).
- Dependency/container vulnerability scanning (`govulncheck`, Trivy on built images) — blocks merge on critical/high findings per the security SLO.
- Contract tests against the published OpenAPI/GraphQL schema — blocks merge on drift.
- Lighthouse CI performance budget checks for the frontend.

All of the above run on every PR via GitHub Actions (see [`hosting-and-deployment.md`](hosting-and-deployment.md) for the full pipeline including deploy).
