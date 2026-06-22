# System Architecture

This document describes how the components chosen in [`docs/tech-stack/`](../tech-stack/overview.md) fit together. For *why* each technology was chosen, see the linked ADRs; this document focuses on *how data flows and where responsibility boundaries sit*.

## High-level flow
```
[OpenSky]      [adsb.lol]      [airplanes.live]
    |               |                |
    v               v                v
 adapter-opensky adapter-adsblol adapter-airplaneslive   <- one Go service each, isolated failure
    \               |                /
     \              |               /
      v             v              v
            ingest.raw.<provider>  (NATS subjects)
                     |
                 normalizer        <- dedup/merge -> canonical FlightState
                     |
              flights.updates (NATS subject)
              /            |              \
             v             v               v
        eventengine   pgstore-writer   apigateway
        (rules ->     (downsampled     (WS broadcaster:
         events.      history +        filters per-client
         detected)    event log)       viewport, fans out)
             |             |               |
             v             v               v
          Postgres      Postgres        Connected
          (events)      (history)       WebSocket clients
                                              |
                                              v
                                    React + MapLibre + deck.gl
                                    (map, search, detail, replay,
                                     event feed, metrics, status)

Redis sits alongside the normalizer (current FlightState + geo index)
and alongside the apigateway (response cache + rate-limit counters).
```

## Component responsibility boundaries
| Component | Owns | Does not own |
|---|---|---|
| Source adapter | Provider auth, polling cadence, rate-limit/backoff compliance, raw payload publish | Dedup, business rules, schema normalization |
| Normalizer | Canonical `FlightState` schema, multi-source dedup/merge precedence, Redis hot-state writes | Event rules, API serving |
| Event engine | Rule evaluation, `Event` emission | Ingestion, serving |
| Postgres writer | Downsampling cadence, durable writes (history + events) | Live query serving (that's the API gateway reading Redis, not Postgres, for current state) |
| API gateway | Auth, rate limiting, caching, REST/GraphQL/WebSocket protocol concerns, viewport-based WS filtering | Business rules (it queries Redis/Postgres, it doesn't recompute flight state) |
| Frontend | Rendering, interaction, client-side viewport state | Any authoritative data — it always reflects what the API/WS gave it |

This table is the basis for code review: a PR that adds, say, geofence-matching logic to the API gateway is adding logic in the wrong place and should move it to the event engine.

## Why ingestion is decoupled from serving (NATS in the middle)
If adapters wrote directly to Redis/Postgres or called the API gateway directly, every new consumer (the event engine today, possibly an analytics job tomorrow) would require touching ingestion code, and a slow consumer could create backpressure all the way up into the provider polling loop. Publishing to NATS JetStream and letting each consumer subscribe independently removes both problems, and gives the replay feature a natural implementation: replay reads from JetStream's retained log (recent window) or Postgres (older, downsampled), rather than needing a bespoke history mechanism.

## Stream bus subjects
See the authoritative table in [`../tech-stack/data-and-messaging.md`](../tech-stack/data-and-messaging.md#nats-jetstream--stream-bus).

## Geospatial query strategy
Two different geospatial needs, two different mechanisms, deliberately not unified into one system:
1. **"What's currently in this viewport?"** (live map queries, REST `bbox` queries) → Redis `GEOSEARCH` against the `flights:geo` set. Fast, simple, matches the "current state of the world" nature of the query.
2. **"Has this aircraft entered/exited a defined zone?"** (geofencing) → point-in-polygon evaluation inside the event engine against `zones` loaded from Postgres (native PostGIS `ST_Contains` if the host supports it, otherwise the `orb` Go library against GeoJSON — see [`data-and-messaging.md`](../tech-stack/data-and-messaging.md#postgresql--durable-store)). This is a per-update rule evaluation, not a live query, so it belongs in the event engine, not the API layer.

## Scalability
- **Scale assumption:** global peak airborne traffic on the order of 10,000–20,000 simultaneously tracked aircraft across combined sources; the architecture targets comfortable headroom to 50,000 tracked entities without structural change.
- **Ingestion scales per-provider, independently.** Each adapter's poll cadence is tuned to that provider's own rate limits; adding a fourth provider means adding a fourth adapter, not touching the other three.
- **Fan-out scales with viewers' viewports, not global aircraft count.** Because the API gateway filters WebSocket pushes per-connection by bbox, a busy single region (e.g., a major hub) — not the global aggregate — is the right worst case to load-test against.
- **Horizontal scaling axis:** add another `apigateway` instance behind the PaaS load balancer for read/WS capacity; ingestion/normalization/event-engine are stateless-where-possible (state lives in Redis/NATS/Postgres, not in-process) so they scale independently of the API tier too.
- **Validated, not assumed:** capacity targets are checked in CI/staging via scripted load tests (simulated viewport churn + simulated ingest volume) per [phase-3-reliability-and-scale.md](../prd/phase-3-reliability-and-scale.md), not claimed from theoretical analysis alone.

## Failure isolation
- A single adapter crashing or being rate-limited degrades coverage/freshness only for the aircraft uniquely seen by that provider — it cannot block the normalizer, other adapters, or the API/frontend (bulkhead pattern, enforced by each adapter being its own process/container with its own NATS subject).
- The frontend distinguishes "stale per-aircraft" (that aircraft's last update is old) from "degraded mode" (overall system freshness has crossed a threshold) — see [`../tech-stack/frontend.md`](../tech-stack/frontend.md#degraded-mode-ui).
- Full failure-mode-by-failure-mode requirements (chaos testing, DR drill) are in [phase-3-reliability-and-scale.md](../prd/phase-3-reliability-and-scale.md).
