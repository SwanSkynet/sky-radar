# Phase 1 PRD: Foundation

## Goal
A real, unmocked, end-to-end pipeline running in production: live data from all three v1 providers, normalized, queryable through a minimal API, and visible on a basic map. No event detection, replay, or advanced UI yet — this phase proves the spine of the system works before anything is layered on top of it.

## In scope
- Source adapters for OpenSky Network, adsb.lol, airplanes.live (see [`../api-docs/`](../api-docs/README.md) for endpoint detail).
- Normalization layer with multi-source dedup/merge (see [`../architecture/data-model.md`](../architecture/data-model.md)).
- Redis hot state store (current `FlightState` + geo index).
- Minimal REST API: `GET /flights` (bbox query), `GET /flights/{icao24}`.
- Basic frontend: MapLibre + deck.gl rendering live aircraft from a polling (not yet WebSocket) REST call.
- CI pipeline: lint, unit tests, container build.
- IaC skeleton: `fly.toml` per service (or chosen PaaS equivalent), `docker-compose.yml` for local dev.

## Out of scope (explicitly deferred)
- NATS JetStream / stream bus (Phase 1 can have the normalizer call Redis directly; the stream bus is introduced in Phase 2 once there's a second consumer that needs decoupling — introducing it before there's a second consumer would be speculative complexity).
- Event engine, replay, watchlists, geofences.
- WebSocket push (frontend polls REST on an interval in Phase 1).
- Public API auth/rate limiting (anonymous-only, generous limits, since there's no abuse surface yet at this stage).
- Observability stack beyond basic health checks (full Grafana Cloud wiring lands in Phase 2 alongside the metrics that are worth dashboarding).

## Functional requirements
| ID | Requirement | Acceptance criteria |
|---|---|---|
| P1-FR1 | Each provider has a source adapter implementing the common `SourceAdapter` interface | Adding/removing a provider requires changes only inside that adapter's package; verified by a CI check that no other package imports an adapter package directly |
| P1-FR2 | Adapters respect documented provider rate limits with backoff | Adapter logs/metrics show zero sustained `429` responses during a 1-hour soak test |
| P1-FR3 | Normalizer merges same-`icao24` reports from multiple providers per the documented precedence rule | Unit tests cover: single-source, multi-source agreement, multi-source conflict (freshness tiebreak), multi-source conflict (quality tiebreak) |
| P1-FR4 | Current state for every tracked aircraft is queryable from Redis by bbox | `GET /flights?bbox=...` returns all aircraft within the box, verified against a known fixture set |
| P1-FR5 | `GET /flights/{icao24}` returns full current state for one aircraft, 404 if not currently tracked | Integration test against the docker-compose stack |
| P1-FR6 | Frontend renders all returned aircraft on a world map via deck.gl over MapLibre | Manual verification + a smoke test that the map mounts and issues a flights request |
| P1-FR7 | A failing/unreachable adapter does not prevent the other two from ingesting | Chaos test: kill one adapter container, verify the other two continue updating Redis |

## Non-functional requirements for this phase
- The system must run unattended for a 24-hour soak test without manual intervention before Phase 1 is considered done.
- `docker-compose up` must bring up a fully working local stack (all 3 adapters against either live calls or recorded fixtures, normalizer, Redis, minimal API, frontend) — this is the project's entire "getting started" experience until Phase 4 docs land.
- No SLO dashboards are required yet, but every service must already expose a basic `/healthz` endpoint, since [phase-2](phase-2-realtime-systems.md)'s observability work builds on this rather than retrofitting it.

## Definition of done
- All three adapters running continuously in the production deployment for 24+ hours with no manual restarts.
- `GET /flights` and `GET /flights/{icao24}` live at a public URL.
- Frontend live at a public URL, showing real aircraft positions updating at least every 10-15 seconds.
- CI green on `main`, covering lint + unit tests + the dedup/merge precedence tests specifically (P1-FR3 is the highest-bug-risk logic in this phase).
