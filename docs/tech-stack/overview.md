# Tech Stack Overview

Every choice below is locked in via an ADR in [`docs/decisions/`](../decisions/) — read the linked ADR for the full rationale and rejected alternatives before proposing a change; changes go through the RFC process described in the Phase 4 packaging and governance PRD.

| Layer                  | Choice                                                                                | Why (short)                                                                                                               | ADR                                                         |
| ---------------------- | ------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------- |
| Backend language       | Go                                                                                    | Concurrency fits poll/fan-out workload, low memory footprint, single-binary deploys, native observability ecosystem       | [0001](../decisions/0001-backend-language-go.md)            |
| Hosting/orchestration  | DigitalOcean Droplet with Docker Compose & Caddy, no Kubernetes                        | Matches volunteer-ops and near-zero-budget constraints                                                                    | [0002](../decisions/0002-hosting-lightweight-containers.md) |
| Stream bus             | NATS JetStream                                                                        | Lightweight, persistent, replayable pub/sub without Kafka's operational weight                                            | [0003](../decisions/0003-stream-bus-nats-jetstream.md)      |
| Frontend map rendering | MapLibre GL JS + deck.gl                                                              | WebGL rendering needed at target aircraft density; free/open-source                                                       | [0004](../decisions/0004-map-rendering-maplibre-deckgl.md)  |
| Hot state / cache      | Redis                                                                                 | Geospatial commands (`GEOADD`/`GEOSEARCH`) give bbox/radius queries without custom indexing; doubles as the query cache   | [`data-and-messaging.md`](data-and-messaging.md)            |
| Durable store          | PostgreSQL                                                                            | Most universally available cheap/free managed Postgres options; sufficient for downsampled history + events at this scale | [`data-and-messaging.md`](data-and-messaging.md)            |
| Frontend framework     | React + TypeScript + Vite                                                             | Widely known, fast dev loop, strong typing matched to the API schema                                                      | [`frontend.md`](frontend.md)                                |
| API protocols          | REST + GraphQL + WebSocket                                                            | REST/GraphQL for queries, WebSocket for live viewport push                                                                | [`backend.md`](backend.md)                                  |
| Observability          | OpenTelemetry instrumentation, Grafana Cloud free tier (metrics/logs/traces/alerting) | Avoids self-hosting a full Prometheus/Grafana/Loki/Tempo stack on volunteer infra                                         | [`observability-and-ops.md`](observability-and-ops.md)      |
| CI/CD                  | GitHub Actions, GitHub Container Registry                                             | Free for public repos, integrates directly with the chosen PaaS deploy flow                                               | [`hosting-and-deployment.md`](hosting-and-deployment.md)    |
| Infra as code          | Docker Compose configs, Caddy configurations, and SSH deploy scripts                  | No Kubernetes manifests to manage; still versioned and reviewable                                                         | [`hosting-and-deployment.md`](hosting-and-deployment.md)    |


## Design principle behind every choice above

Every selection optimizes for the same three constraints simultaneously, because relaxing any one of them changes the answer:

1. **No revenue** → cost must stay near-zero or sponsor-funded, with a budget cap and alerting, not an afterthought.
2. **Volunteer maintainers** → operational complexity has to be something a small group can run without dedicated ops staff.
3. **Production-grade as a goal in itself** → none of the above is an excuse to skip SLOs, testing, observability, or security — the constraint is _how_ those are achieved (e.g., Grafana Cloud's free tier instead of self-hosted Grafana, not "skip observability").

## Where to look next

- Backend services in detail (repo layout, frameworks, libraries): [`backend.md`](backend.md)
- Frontend in detail: [`frontend.md`](frontend.md)
- NATS/Redis/Postgres usage patterns and schemas: [`data-and-messaging.md`](data-and-messaging.md)
- CI/CD, monitoring, alerting, on-call: [`observability-and-ops.md`](observability-and-ops.md)
- Deployment topology and environments: [`hosting-and-deployment.md`](hosting-and-deployment.md)
- How these pieces fit together end-to-end: [`../architecture/system-architecture.md`](../architecture/system-architecture.md)
