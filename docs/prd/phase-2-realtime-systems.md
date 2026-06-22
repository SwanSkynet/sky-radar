# Phase 2 PRD: Real-time Systems Depth

## Goal
Turn the Phase 1 spine into the actual product: live push instead of polling, event detection, replay, search/filter, watchlists/geofences, a public API with auth and rate limiting, and the observability stack that makes the system's behavior visible. This phase is where most of the project's "systems design" signal lives.

## Prerequisite
Phase 1 complete and stable (24-hour soak passed) — this phase adds a stream bus and several new consumers on top of the Phase 1 normalizer, so the underlying ingest/merge logic needs to already be trustworthy.

## In scope
- **NATS JetStream** introduced as the stream bus (see [`../tech-stack/data-and-messaging.md`](../tech-stack/data-and-messaging.md)); normalizer now publishes to `flights.updates` instead of (or in addition to) writing Redis directly.
- **Event engine**: altitude delta, speed delta, stale-signal, geofence enter/exit, watchlist match rules; emits to `events.detected`; writes to Postgres `events` table.
- **Postgres durable store**: `flight_history` (downsampled), `events`, `zones` tables live; DB writer consumes `flights.updates` and `events.detected`.
- **WebSocket viewport subscriptions**: API gateway subscribes to `flights.updates`, filters per connected client by registered bbox, pushes updates — frontend switches from polling to this.
- **Replay**: reconstructs recent movement from JetStream's retained window / Postgres downsampled history; frontend replay scrubber.
- **Search and filters**: callsign/registration/ICAO24 search, altitude/speed band, region, event-type filters.
- **Watchlists and geofences**: user-defined (session-scoped) zones and tracked entities; in-app notification on match.
- **Public API v1**: REST + GraphQL, versioned (`/api/v1`), API-key auth for elevated rate limits, anonymous tier for casual use, OpenAPI/GraphQL schema published and contract-tested.
- **Caching**: Redis-backed response caching for viewport/detail queries.
- **Observability stack live**: OpenTelemetry instrumentation, Grafana Cloud wiring, SLO dashboards for every metric in the [master PRD](../prd/00-master-prd.md#8-non-functional-requirements-slos).

## Out of scope (deferred to Phase 3)
- Formal chaos/load testing against capacity targets (this phase implements the features; Phase 3 proves they hold under failure/load).
- Disaster recovery drills.
- Security review/pen-test pass (basic input validation and rate limiting ship now; the formal review is Phase 3).

## Functional requirements
| ID | Requirement | Acceptance criteria |
|---|---|---|
| P2-FR1 | Event engine evaluates every canonical update against all five rule types | Unit tests per rule type with both triggering and non-triggering fixtures |
| P2-FR2 | Events are persisted with timestamp and reference to the aircraft/zone | Integration test: trigger each event type end-to-end, verify the resulting Postgres row |
| P2-FR3 | WebSocket clients receive only updates within their registered viewport | Test harness opens connections with different bboxes, asserts each only receives in-bbox updates |
| P2-FR4 | Reconnecting WebSocket clients can resume without a full state reload | Client sends last-seen sequence/offset; gateway replays the gap from JetStream where still retained |
| P2-FR5 | Replay reconstructs the last N minutes of movement, visually distinct from live mode | Manual + integration test against seeded historical data |
| P2-FR6 | Search/filter results update within the latency budget | Verified against the API latency SLO in the master PRD |
| P2-FR7 | Watchlist/geofence matches surface as in-app notifications without a page reload | Manual verification + event-emission test |
| P2-FR8 | Public API is versioned, documented, and rate-limited per key tier | Contract tests against published schema; rate-limit enforcement test (429 + Retry-After) |
| P2-FR9 | Cache hit rate is measurable and visible on the metrics dashboard | Dashboard panel backed by the cache-hit-rate metric, sanity-checked against real traffic |
| P2-FR10 | Every SLO in the master PRD has a corresponding dashboard panel | Dashboard reviewed against the SLO table line by line |

## Non-functional requirements for this phase
- Event detection latency P95 ≤ 5s (master PRD target) — measured and shown on the dashboard, not just assumed.
- WebSocket per-connection bandwidth must not scale with global aircraft count — verified by a manual test comparing bandwidth for a small-bbox vs. large-bbox client under the same global load.
- Cache hit rate ≥ 60% target — tracked from day one of this phase, even before it's optimized to hit the target, so there's a baseline to improve against.

## Definition of done
- Live map runs on WebSocket push, no polling.
- All five event types observably firing in production against real traffic.
- Replay works for at least the previous 30 minutes.
- Public API v1 is live, documented, and has at least the project's own frontend as a "first customer" proving the API and the UI aren't secretly coupled to private internals.
- Every SLO has a real (even if not-yet-met) number on a public-readable dashboard.
