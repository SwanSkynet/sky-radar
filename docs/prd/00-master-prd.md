# Sky Radar — Master PRD

## 0. Document purpose
This is the top-level product requirements document for Sky Radar: a production-grade, open-source, real-time airspace intelligence platform. There is no revenue model — Sky Radar is built and maintained as a public open-source project (repo: `sky-radar`), funded by donated/free-tier infrastructure and optional community sponsorship rather than billing. "Production-grade" means the system is engineered to the standard of a real, internet-facing, globally available service: it has SLOs, on-call-style operational practices, security review, capacity planning, disaster recovery, automated testing and deployment, and a sustainable cost model — not just a working demo.

This document defines product scope, requirements, and SLOs. It intentionally does **not** repeat implementation detail that lives in dedicated docs:
- Technology choices and rationale → [`docs/tech-stack/`](../tech-stack/overview.md)
- Why each major technology was chosen → [`docs/decisions/`](../decisions/) (ADRs)
- Detailed system design → [`docs/architecture/system-architecture.md`](../architecture/system-architecture.md)
- Canonical schemas → [`docs/architecture/data-model.md`](../architecture/data-model.md)
- Phase-by-phase detailed requirements and acceptance criteria → [`docs/prd/phase-1-foundation.md`](phase-1-foundation.md) through [`phase-4-packaging-and-governance.md`](phase-4-packaging-and-governance.md)
- Task-level build sequencing → [`docs/implementation-plan.md`](../implementation-plan.md)

## 1. Overview
Sky Radar ingests live aircraft state data from multiple public ADS-B/MLAT data providers (OpenSky Network, adsb.lol, airplanes.live), normalizes it into a single consistent flight model, detects notable events (sudden altitude/speed changes, geofence crossings, stale signals), and serves it to a web client that renders global air traffic on an interactive map in near real time, with historical replay and a public read API. The system is designed to operate continuously, unattended, at global scale (tens of thousands of concurrently tracked aircraft), and to degrade gracefully when upstream data sources are slow, rate-limited, or unavailable.

## 2. Goals and non-goals
### Goals
- Ingest, normalize, and serve live global aircraft state with sub-15-second end-to-end freshness under normal conditions.
- Operate as a resilient, multi-provider system — no single upstream data source is a single point of failure.
- Meet explicit SLOs for availability, latency, and data freshness, and expose them publicly via a status page.
- Provide a versioned, rate-limited public read API so the data is reusable by other developers, not just the bundled frontend.
- Be operable by a small group of volunteer maintainers: deployments, rollbacks, and incident response must be low-toil and well-documented.
- Run within a near-zero/low operating cost envelope using free tiers, caching, and efficient data structures, with cost as a first-class design constraint.
- Be a credible reference implementation of real-time, event-driven, geospatial systems design that the community can learn from, fork, and extend.

### Non-goals
- No user accounts tied to billing, no payment processing, no paid tiers.
- No proprietary/licensed data redistribution that violates source provider terms of service.
- Not aiming to match the coverage, latency, or completeness of commercial providers (FlightAware, FlightRadar24, ADS-B Exchange) — Sky Radar is additive/derivative, built on top of public data, not a replacement for them.
- No mobile native apps in v1 (web is mobile-responsive instead).
- No support for non-aviation domains (maritime/AIS, etc.) in v1, even though the architecture is designed to generalize later.
- No Kubernetes/multi-region active-active infrastructure in v1 — see [ADR-0002](../decisions/0002-hosting-lightweight-containers.md) for why a lightweight single/few-node deployment is the deliberate choice, not a temporary shortcut.

## 3. Background and problem statement
Public ADS-B data is fragmented across multiple community-run aggregators (OpenSky Network, adsb.lol, airplanes.live), each with different coverage, latency, rate limits, and terms of use. No single free source guarantees global, continuous, high-frequency coverage on its own. A production-grade open system has to (a) combine multiple imperfect sources into one coherent view, (b) be honest about data quality and staleness rather than presenting a false sense of completeness, and (c) do all of this within free/low-cost infrastructure limits, which is a materially different engineering problem than building the same system with an unlimited cloud budget.

## 4. Users and use cases
| User | Use case | Needs |
|---|---|---|
| Aviation enthusiast / general public | Browse live air traffic, look up flights, watch specific aircraft | Fast map, search, mobile-friendly UI, accurate-enough data with honest staleness indicators |
| Developer / API consumer | Build on top of Sky Radar's public API (bots, dashboards, research) | Stable versioned API, documented rate limits, predictable uptime, clear licensing of derived data |
| Open-source contributor | Read code, fix bugs, add features, run it locally | Clear architecture docs, local dev setup, CI that gives fast feedback, contribution guidelines |
| Maintainer / operator | Keep the system running, respond to incidents, manage cost | Dashboards, alerts, runbooks, infra-as-code, cost visibility |
| Engineers reviewing the project | Evaluate system design depth and production maturity | Architecture docs, SLOs, postmortems/lessons-learned, test coverage, observability |

## 5. Data sources and licensing
Sky Radar treats data provenance and licensing as a hard constraint, not an afterthought. Full provider details (endpoints, auth, rate limits, fields) live in [`docs/api-docs/`](../api-docs/README.md).

- **v1 sources:** OpenSky Network REST API, adsb.lol, airplanes.live — all free, no-payment community/research feeds.
- **Source abstraction requirement:** every provider is implemented behind a common `SourceAdapter` interface (poll in, normalize out) so sources can be added, removed, or disabled per-region without touching downstream code. This is both an engineering requirement ([FR-1](phase-1-foundation.md)) and a compliance requirement — a source whose terms change must be removable in one deploy.
- **Attribution:** every served flight record carries a `sources` field, and a public "Data Sources & Attribution" page credits upstream providers per their terms.
- **No augmentation that creates new compliance risk:** Sky Radar does not purchase or scrape licensed commercial feeds (e.g., FlightAware AeroAPI, Cirium, ADS-B Exchange's commercial tier) in v1, since that would introduce billing and ToS obligations incompatible with the no-revenue, open-source model.

## 6. System architecture (summary)
Ingestion (per-provider adapters) → normalization (canonical `FlightState`) → NATS JetStream (decoupling/replay) → event engine + hot state (Redis) + durable store (Postgres) → API gateway (REST/GraphQL/WebSocket) → frontend (MapLibre + deck.gl).

Full diagrams, component responsibilities, and design rationale: [`docs/architecture/system-architecture.md`](../architecture/system-architecture.md). Schemas: [`docs/architecture/data-model.md`](../architecture/data-model.md). Stack choices and why: [`docs/tech-stack/overview.md`](../tech-stack/overview.md).

## 7. Functional requirements (index)
Detailed, testable requirements are organized by delivery phase rather than listed flat here, since each phase's requirements depend on the previous phase's data being live. See:
- [Phase 1 — Foundation](phase-1-foundation.md): source adapters, normalization, basic state store, minimal API, basic map.
- [Phase 2 — Real-time systems depth](phase-2-realtime-systems.md): event engine, replay, WebSocket viewport subscriptions, caching, public API v1.
- [Phase 3 — Reliability and scale hardening](phase-3-reliability-and-scale.md): failure isolation, fail-soft UI, load/chaos testing, DR.
- [Phase 4 — Packaging and governance](phase-4-packaging-and-governance.md): status page, docs, postmortem process, contributor on-ramp.

## 8. Non-functional requirements (SLOs)
| Dimension | Target | Notes |
|---|---|---|
| Availability (API + frontend) | 99.5% monthly | Realistic for a volunteer-operated, free-tier-hosted system; tracked and published, not aspirational-only |
| Data freshness (P95) | ≤ 15s behind source under normal conditions; degraded-mode banner if > 60s | Measured per source and end-to-end |
| Event detection latency (P95) | ≤ 5s from normalized ingest to event emission | |
| Map interaction latency | ≤ 150ms for pan/zoom/filter on a mid-tier device | Frontend perf budget, profiled in CI |
| API latency (P95, cached reads) | ≤ 200ms | |
| API latency (P95, uncached/history queries) | ≤ 800ms | |
| WebSocket fan-out | Per-connection bandwidth bounded by viewport regardless of global aircraft count | |
| Cache hit rate (viewport/query reads) | ≥ 60% | Directly controls upstream-provider call volume and cost |
| Disaster recovery — RPO | ≤ 5 minutes for durable history/event store | |
| Disaster recovery — RTO | ≤ 1 hour for full service restore | |
| Security | No known critical/high CVEs in production dependencies for > 7 days | Automated scanning on every build |
| Cost | Stay within declared free-tier/sponsor-funded budget; alert before the cap is hit | First-class constraint given no-revenue model |

These SLOs, and current attainment, are published on a public status page — an explicit product requirement, not just an internal target, because transparency about real production behavior (including misses) is part of what makes this a credible engineering artifact.

## 9. Success metrics
| Category | Metric | Target |
|---|---|---|
| Reliability | Monthly availability | ≥ 99.5%, published |
| Data quality | P95 freshness | ≤ 15s, published |
| Performance | Map interaction latency | ≤ 150ms |
| Efficiency | Cache hit rate | ≥ 60% |
| Cost | Monthly infra spend | Within declared budget cap |
| Community | Public API adopters, contributors, issues resolved | Tracked and reported quarterly in repo |
| Engineering credibility | Test coverage on core ingestion/event logic, postmortems published, SLO transparency | Qualitative but explicitly evaluated |

## 10. Delivery roadmap
| Phase | Focus | Detail |
|---|---|---|
| 1 — Foundation | Real (not mocked) end-to-end pipeline in production before advanced features | [phase-1-foundation.md](phase-1-foundation.md) |
| 2 — Real-time systems depth | Event engine, replay, live push, caching, public API v1 | [phase-2-realtime-systems.md](phase-2-realtime-systems.md) |
| 3 — Reliability and scale hardening | Failure isolation, load/chaos testing, DR drill, security review | [phase-3-reliability-and-scale.md](phase-3-reliability-and-scale.md) |
| 4 — Packaging and governance | Status page, full docs, postmortem template, contributor on-ramp | [phase-4-packaging-and-governance.md](phase-4-packaging-and-governance.md) |

Task-level sequencing within and across phases: [`docs/implementation-plan.md`](../implementation-plan.md).

## 11. Risks and mitigations
| Risk | Impact | Mitigation |
|---|---|---|
| Upstream provider changes API/ToS or shuts down | Coverage loss, possible compliance issue | Source-adapter abstraction, multi-provider redundancy, contract tests catch breakage fast |
| Free-tier infra limits exceeded under real traffic | Service degradation or unexpected cost | Cost dashboard + budget alerts, self-host/lite mode as pressure relief, viewport-scoped fan-out caps per-client cost |
| Volunteer maintainer bandwidth is limited | Slow incident response, stale roadmap | Strong runbooks/docs to lower response toil, lightweight governance to enable outside contributors to pick up load |
| Public API abused (scraping/DoS) | Degraded service for everyone | Rate limiting, bounded queries, WAF/CDN front door |
| Data quality issues (MLAT noise, source disagreement) presented as fact | Misleads users, undermines credibility | Position-quality field, multi-source dedup precedence, explicit staleness/degraded-mode UI |
| Scope creep beyond v1 | Delays first production-quality release | Phase-gated roadmap; features outside the phase FR list require an RFC before being added |

## 12. Appendix
**Glossary:** ADS-B (Automatic Dependent Surveillance–Broadcast), MLAT (Multilateration), ICAO24 (unique 24-bit aircraft transponder address), bbox (bounding box query).
