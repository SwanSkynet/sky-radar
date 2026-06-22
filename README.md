# Sky Radar

Sky Radar is an open-source, real-time airspace intelligence platform. It ingests live aircraft state data from multiple public ADS-B/MLAT providers, normalizes it into one consistent flight model, detects notable events (sudden altitude/speed changes, geofence crossings, stale signals), and serves it to a web client that renders global air traffic on an interactive map with historical replay and a public read API.

There is no revenue model — this is a community-maintained, free/low-cost-infrastructure project, engineered to production-grade standards (SLOs, observability, automated testing/deployment, security review, disaster recovery) rather than a one-off demo.

> **Status: pre-implementation / planning.** Architecture and scope are settled; code has not landed yet. The roadmap below outlines what's coming first.

## What it does (planned v1)

- **Live global map** of currently tracked aircraft, with clustering/level-of-detail so the UI stays responsive under high traffic density.
- **Flight detail view** — position, altitude, speed, heading, callsign, data source(s), and a staleness indicator.
- **Search and filters** by callsign/registration/ICAO24, altitude band, speed band, region, and event type.
- **Event feed** — rapid altitude/speed changes, stale signals, geofence enter/exit, watchlist matches.
- **Watchlists and geofences** — user-defined zones and tracked aircraft with in-app notifications.
- **Time replay** of recent movement history, clearly distinguished from live mode.
- **Public read API** (REST + GraphQL + WebSocket) — versioned, rate-limited, documented.
- **Live engineering metrics and status page** — ingest throughput, freshness, latency, cache hit rate, SLO attainment, published openly.

## Architecture (target)

```
[Provider A] [Provider B] [Provider C] [Community feeders]
        \        |         |          /
         v        v         v         v
              Source Adapters (per-provider, isolated failure)
                          |
                  Normalization Layer
              (canonical Flight State schema)
                          |
                 Stream Bus (pub/sub, replayable)
                 /                \
        Event Engine          State Store
   (rules: altitude/speed     (hot in-memory +
    delta, geofence,           durable timeseries
    stale detection,            for replay/history)
    watchlist match)
        \                /
              API Gateway
       (REST + GraphQL + WebSocket,
        auth, rate limiting, caching)
                  |
            Frontend (map, search,
            detail, replay, metrics,
            architecture/status pages)
```

Each data provider sits behind a common adapter interface so a provider can be added, disabled, or rate-limited without touching downstream code, and so one provider failing never takes down the rest of the system.

## Data sources and attribution

Sky Radar is built entirely on public/community aviation data feeds (e.g., OpenSky Network, adsb.lol, airplanes.live) and does not redistribute licensed commercial data. Every served record will carry a `sources` field, and a "Data Sources & Attribution" page will credit upstream providers per their terms.

## Roadmap

| Phase | Focus |
|---|---|
| 1 — Foundation | Source adapters, normalization, basic state store, minimal API, basic map frontend, CI, IaC skeleton |
| 2 — Real-time systems depth | Stream bus, event engine, replay, WebSocket viewport subscriptions, caching, observability stack, public API v1 |
| 3 — Reliability and scale hardening | Failure isolation, fail-soft UI, load/chaos testing, DR drill, security review |
| 4 — Packaging and governance | Live status/architecture page, full docs, postmortem template, contributor on-ramp, cost dashboard |

## Getting started

Local development setup (docker-compose stack with mocked provider data) will be documented here once Phase 1 lands.

One-time setup: run `git config core.hooksPath .githooks` to enable the formatting pre-commit hook.

## Contributing

Contribution guidelines, coding standards, and the architecture-change RFC process will live in `CONTRIBUTING.md`. Until then, open an issue to discuss ideas or pick up roadmap items.

## License

License to be finalized (permissive, e.g., MIT or Apache-2.0) before the first code lands.
