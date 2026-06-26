# Sky Radar

Sky Radar is an open-source, real-time airspace intelligence platform. It ingests live aircraft state data from public ADS-B/MLAT providers, normalizes it into one canonical flight model, detects notable events, and serves the data through a web client and public API.

This repository is a production-grade project in planning/implementation, not a demo. Treat every change as if it will ship to a public service with reliability, observability, and cost constraints.

## Monorepo Structure

The repository is organized as a Go-first monorepo with a web frontend and deployment assets:

```text
/cmd
  /adapter-opensky
  /adapter-adsblol
  /adapter-airplaneslive
  /normalizer
  /eventengine
  /apigateway
/internal
  /flightmodel
  /sourceadapter
  /natsutil
  /redisutil
  /pgstore
  /pgstorewriter
  /geo
/web
/deploy
/docs
/prompts
```

Guiding rule: `/cmd` binaries are independently deployable processes. Shared code lives only in `/internal`; do not create direct dependencies between service binaries.

## Locked Tech Stack

Treat these decisions as fixed unless I explicitly request a change:

- Backend language: Go
- Hosting/orchestration: DigitalOcean Droplet with Docker Compose & Caddy, no Kubernetes
- Stream bus: NATS JetStream
- Hot state/cache: Redis
- Durable store: PostgreSQL
- Frontend: React + TypeScript + Vite
- Map rendering: MapLibre GL JS + deck.gl
- State management: Zustand
- Styling: Tailwind CSS
- API protocols: REST + GraphQL + WebSocket
- Observability: OpenTelemetry + Grafana Cloud free tier
- CI/CD: GitHub Actions + GitHub Container Registry
- Infra as code: Docker Compose configs, Caddy configurations, and SSH deploy scripts

## How To Run

Use these commands when the relevant tooling exists in the repo:

- Lint: `golangci-lint run ./...`
- Go tests: `go test ./...`
- Local stack: `docker compose up --build`
- Prod stack local run: `docker compose -f deploy/docker-compose.prod.yml up --build`


For frontend work, use the scripts defined in `web/package.json` once the web app exists. Prefer the repo's documented commands over inventing new ones.

## Key Docs

Read these docs as the source of truth before making changes:

- `README.md` — project overview and public-facing summary
- `docs/implementation-plan.md` — build order and milestone sequencing
- `docs/prd/00-master-prd.md` — product goals, scope, SLOs, and roadmap
- `docs/prd/phase-1-foundation.md` — Phase 1 requirements and definition of done
- `docs/prd/phase-2-realtime-systems.md` — Phase 2 realtime systems requirements
- `docs/prd/phase-3-reliability-and-scale.md` — Phase 3 reliability, chaos, and DR requirements
- `docs/prd/phase-4-packaging-and-governance.md` — Phase 4 docs, governance, and contributor on-ramp
- `docs/architecture/system-architecture.md` — component boundaries and data flow
- `docs/architecture/data-model.md` — canonical schemas and merge rules
- `docs/api-docs/README.md` — provider comparison and adapter entry point
- `docs/api-docs/opensky-api-docs.md` — OpenSky provider details
- `docs/api-docs/adsb-lol-api-docs.md` — adsb.lol provider details
- `docs/api-docs/airplanes-live-docs.md` — airplanes.live provider details
- `docs/tech-stack/overview.md` — stack decisions at a glance
- `docs/tech-stack/backend.md` — backend repo layout and service boundaries
- `docs/tech-stack/data-and-messaging.md` — NATS, Redis, and Postgres usage
- `docs/tech-stack/frontend.md` — frontend architecture and UI constraints
- `docs/tech-stack/observability-and-ops.md` — metrics, alerts, dashboards, and runbooks
- `docs/tech-stack/hosting-and-deployment.md` — environments, deployment topology, and CI/CD
- `docs/decisions/0001-backend-language-go.md` — Go backend rationale
- `docs/decisions/0002-hosting-lightweight-containers.md` — lightweight hosting rationale
- `docs/decisions/0003-stream-bus-nats-jetstream.md` — NATS JetStream rationale
- `docs/decisions/0004-map-rendering-maplibre-deckgl.md` — map rendering rationale

## Standing Instructions For Every Claude Code Session

- Treat the architecture, PRDs, implementation plan, and tech stack docs as the source of truth.
- Treat the stack decisions above as locked unless I explicitly request otherwise.
- Start from the smallest concrete code path that controls the requested behavior.
- Before the first substantive edit, gather only enough local evidence to form one falsifiable hypothesis and one cheap check.
- Make the smallest mergeable change that solves the task at the root cause.
- Do not widen scope into adjacent refactors unless they are required to make the requested milestone correct.
- After the first substantive edit, run the narrowest useful validation immediately before doing more work.
- Add or update tests for every behavior change.
- If a command, validation, or tool exists for a task, use it instead of inventing a workaround.
- Do not revert unrelated user changes.
- Do not make destructive git operations.
- Prefer concise, factual progress updates while working.

## Default Deliverable Format

When I ask you to implement something, finish with:

- A short summary of what changed
- The files changed
- The validation commands you ran
- Any failures, gaps, or follow-up items that remain

Keep the final response concise unless I ask for depth.