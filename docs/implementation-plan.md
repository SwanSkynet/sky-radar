# Implementation Plan

This breaks each phase PRD into a build order. Sizing is relative effort (S / M / L), not calendar time — this is a volunteer-paced project, and committing to dates here would be a guess dressed up as a plan. Each milestone within a phase should be a mergeable, demoable increment, not a single giant PR.

## Phase 1 — Foundation

1. **Repo scaffolding** (S) — monorepo layout per [`tech-stack/backend.md`](tech-stack/backend.md), `docker-compose.yml` skeleton, CI workflow shell (lint + test, no deploy yet).
2. **Canonical types** (S) — `FlightState` in `/internal/flightmodel`, matching [`architecture/data-model.md`](architecture/data-model.md) exactly; this unblocks every other task in parallel.
3. **One source adapter end-to-end** (M) — pick the simplest provider first (airplanes.live: no auth required) and get raw payload → Redis publishing working directly (NATS isn't introduced until Phase 2, so this writes straight through for now; the Phase 2 version will map that stream to `ingest.raw.airplaneslive`) plus fixture-based contract tests. This proves the adapter pattern before replicating it twice more.
4. **Remaining two adapters** (M) — adsb.lol, then OpenSky (OpenSky needs OAuth2 client-credentials handling, so it's deliberately last/hardest).
5. **Normalizer + merge logic** (M) — dedup/merge precedence rule, unit-tested against all the conflict scenarios in [phase-1-foundation.md](prd/phase-1-foundation.md#functional-requirements) before moving on.
6. **Minimal REST API** (S) — `GET /flights`, `GET /flights/{icao24}` against Redis.
7. **Basic frontend** (M) — MapLibre + deck.gl rendering polled REST data; this is the first genuinely demoable artifact.
8. **Deploy to production** (M) — pick the hosting platform per [`tech-stack/hosting-and-deployment.md`](tech-stack/hosting-and-deployment.md), wire CI deploy, get a public URL live.
9. **24-hour soak test** (S, but calendar-blocking) — Phase 1 isn't done until this passes unattended.

## Phase 2 — Real-time Systems Depth

1. **Introduce NATS JetStream** (M) — normalizer publishes to `flights.updates`; this is a refactor of an already-working Phase 1 path, not new business logic, so it should be low-risk.
2. **Event engine, one rule at a time** (M, repeated x5) — build and ship stale-signal detection first (simplest, no thresholds to tune), then altitude/speed delta, then geofence, then watchlist match last (depends on the zones/watchlist data model existing).
3. **Postgres durable store + writer** (M) — schema migration, downsampled history writer, events writer.
4. **WebSocket gateway** (L) — viewport-scoped subscription, reconnect/resume semantics; this is the highest-complexity item in the phase and worth its own design review before merging.
5. **Frontend: switch to WebSocket** (M) — replace polling; this is also where the degraded-mode/staleness UI groundwork goes in, even though it's only fully exercised in Phase 3.
6. **Replay** (M) — backend read path against JetStream retention + Postgres downsampled history; frontend scrubber.
7. **Search/filter, watchlists, geofences** (M, can parallelize across contributors since these touch different code paths) — these are independent product features that each plug into the existing API/event pipeline.
8. **Public API v1 + auth + rate limiting** (M) — versioning, OpenAPI/GraphQL schema publication, contract tests in CI.
9. **Observability stack wiring** (M) — OpenTelemetry instrumentation across all services, Grafana Cloud connection, one dashboard panel per SLO.

## Phase 3 — Reliability and Scale Hardening

1. **Load test harness** (M) — build the simulated-viewport-churn and simulated-ingest-volume scripts before trying to pass any target with them.
2. **Run load tests, fix what breaks** (L, iterative) — this is the item most likely to reveal real architectural surprises; budget for it taking longer than it looks.
3. **Chaos test harness + scripted failure injection** (M) — automate the kill-and-observe scenarios in [phase-3-reliability-and-scale.md](prd/phase-3-reliability-and-scale.md).
4. **Fail-soft UI correctness pass** (S) — verify against real degraded conditions, not just the happy-path implementation from Phase 2.
5. **DR drill** (M, scheduled as a deliberate exercise, not squeezed in) — actually restore from backup and redeploy from IaC; record real timings.
6. **Security review pass** (M) — threat model walkthrough, dependency scan gating if not already enforced, scripted abuse test against rate limiting, publish `SECURITY.md`.
7. **Write the phase report** (S) — consolidate all of the above into the reliability/postmortem-style report called for in the phase's definition of done.

## Phase 4 — Packaging and Governance

1. **License decision + `LICENSE` file** (S) — should happen early in this phase since it gates how confidently the project can be promoted/shared.
2. **Status/architecture page** (M) — wire the public-readable dashboard views built in Phase 2/3 into a page on the actual frontend.
3. **README/CONTRIBUTING/CODE_OF_CONDUCT/SECURITY finalization** (M) — README rewritten to describe the real running system; the others drafted fresh.
4. **Runbooks for every existing alert** (M) — an alert without a runbook is a defect per [`tech-stack/observability-and-ops.md`](tech-stack/observability-and-ops.md); this is the cleanup pass that closes that gap.
5. **RFC template + first real RFC** (S) — use it for whatever decision is still genuinely open at this point, rather than manufacturing a fake one.
6. **Postmortem template + Phase 3's DR drill written up in that format** (S) — gives the template a real first example.
7. **Cost dashboard goes public** (S) — should already exist internally from Phase 2/3 cost tracking; this step is making it a public-facing panel.
8. **Dry-run onboarding test** (M) — have someone unfamiliar with the codebase try to get a local stack running from docs alone; fix whatever they get stuck on.

## Phase 5 — Map UX Batch 1 (post-Phase-4 feature work)

Detailed spec: [`features/batch-1-coverage-detail-icons-cadence.md`](features/batch-1-coverage-detail-icons-cadence.md).
Prompts: [`prompts/phase-5/`](../prompts/phase-5/). Built in this order because
the later items depend on the earlier ones (type data must exist before icons can
draw it).

1. **Coverage & global ingest** (S) — config-only: OpenSky runs credentialed +
   global, regional adapters pinned to a deliberate region (SoCal), tighter
   cadence. Fixes the accidental SF-only live map. Ingestion is shared/server-side
   and does **not** follow a user's camera; global coverage comes from OpenSky.
2. **Aircraft type capture + classifier** (M) — capture type/category/military
   from adsb.lol & airplanes.live, add nullable fields to `FlightState`
   (data-model updated), plumb through Redis/API/WS, seed classifier → icon
   buckets. Backend half of the icon feature.
3. **Flight detail panel** (M) — clickable aircraft + live-updating detail drawer.
4. **Geolocation initial center** (S) — open the map on the visitor's location,
   world-view fallback; a per-user view default only.
5. **Per-type SVG icons** (M) — `IconLayer` with heading rotation, class→SVG
   from `web/src/assets`, default `commercial_jet`, staleness preserved.
6. **Faster & smoother updates** (S) — client-side dead-reckoning interpolation
   plus the tightened cadence from item 1.

Deferred to a later batch: the full ICAO type-designator DB / military hex-range
tables (this batch ships a small seed classifier), and a demand-following /
multi-point regional poll grid.

## Cross-cutting notes

- **Don't start Phase 2's event engine before Phase 1's merge logic has run in production for a while.** Event rules operate on the normalizer's output; if the merge logic has subtle bugs, building event rules on top of it just compounds the debugging surface later.
- **NATS is introduced in Phase 2, not Phase 1, on purpose** — Phase 1 has exactly one consumer of normalized data (the minimal API reading Redis), so a stream bus would be decoupling something that doesn't need decoupling yet. This is consistent with the project's general principle of not building for a need that doesn't exist yet.
- **Every phase's "definition of done" in its PRD is the actual gate** — this plan is a suggested path through that gate, not a substitute for it.
