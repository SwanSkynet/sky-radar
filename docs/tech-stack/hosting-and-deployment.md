# Hosting and Deployment

See [ADR-0002](../decisions/0002-hosting-lightweight-containers.md) for why this avoids Kubernetes.

## Environments

| Environment            | Purpose                                       | Data sources                                               | Scale                                                                           |
| ---------------------- | --------------------------------------------- | ---------------------------------------------------------- | ------------------------------------------------------------------------------- |
| Local (docker-compose) | Development, the default self-host/lite mode  | Recorded fixture payloads or live calls at reduced cadence | Single instance of everything                                                   |
| Staging                | Pre-production validation, load/chaos testing | Live providers, reduced traffic                            | Matches production topology at smaller size                                     |
| Production             | The public deployment                         | Live providers                                             | Sized to [capacity targets](../architecture/system-architecture.md#scalability) |

## Hosting platform

**Primary candidate: Fly.io.** Runs containers as small VMs close to users, has a usable free/cheap tier, supports persistent volumes (for Redis/Postgres if not using a separate managed DB add-on), and deploys via a simple `fly deploy` from a `fly.toml` per service — fitting the "small number of always-on lightweight instances" model from ADR-0002.

**Acceptable alternatives** (documented here so a maintainer can switch without re-deriving the reasoning): Railway (similarly simple, PaaS-managed Postgres/Redis add-ons), or a single VPS (e.g., Hetzner/DigitalOcean smallest tier) running the same containers via `docker-compose` plus `systemd` for restart-on-failure — the most manual option, but the cheapest and the one with zero platform lock-in.

**Managed data stores:** prefer a managed free/cheap-tier Postgres (e.g., a hosting provider's own Postgres add-on, or Neon/Supabase free tier) over self-managing Postgres on a VM, since backups and the DR story in the [master PRD](../prd/00-master-prd.md#8-non-functional-requirements-slos) are then partially the provider's responsibility. Redis and NATS, being lighter and more disposable (hot state can be rebuilt from the live feed; the stream bus's retention window is short), can run as containers alongside the application services.

## Deployment topology (initial)

```
Instance 1: adapter-opensky, adapter-adsblol, adapter-airplaneslive, normalizer
Instance 2: eventengine, pgstore-writer, apigateway
Instance 3: NATS JetStream, Redis
Managed Postgres (provider add-on, not self-hosted)
Static frontend: deployed to a CDN/static host (e.g., the PaaS's static asset serving, or Cloudflare Pages free tier)
```

This topology is a starting point, not a permanent constraint — instances are added horizontally (another `apigateway` instance behind the PaaS's load balancer, for example) as the [capacity plan](../architecture/system-architecture.md#scalability) requires, without changing the no-Kubernetes decision.

## Intended CI/CD pipeline (GitHub Actions)

1. **On every PR:** lint, unit tests, integration tests (docker-compose stack with mocked providers), contract tests, container build (no push), Lighthouse CI for frontend changes.
2. **On merge to `main`:** build and push images to GitHub Container Registry (`ghcr.io`, free for public repos), deploy to staging automatically.
3. **Production promotion:** manual approval gate (a GitHub Environment protection rule) promotes the staging build to production, deployed with the PaaS's rolling/canary deploy if available, with an automatic rollback on failed health checks.

## Infra as code

- Application deployment config (`fly.toml` or equivalent) is committed per service under `/deploy`.
- Terraform is used only for ancillary cloud resources the PaaS doesn't manage itself (e.g., DNS records, a CDN/WAF in front of the public API, object storage for backups) — kept intentionally small given there's no Kubernetes cluster or complex networking to define.
- `docker-compose.yml` at the repo root will define the full local-dev stack (every service + NATS + Redis + Postgres + Grafana/Prometheus, per [`observability-and-ops.md`](observability-and-ops.md)) — this will also be the reference topology for anyone self-hosting.

## Cost cap

A declared monthly budget cap (finalized once a specific provider/tier is selected during Phase 1) is tracked on the cost dashboard described in [`observability-and-ops.md`](observability-and-ops.md), with an alert before the cap is reached. Until real production traffic data exists, the cap should be set conservatively and revisited after the first month of live operation.

## Phase 1 production deployment (current)

The platform decision above is now concrete for Phase 1: **Fly.io**, region `sjc`. Per-service config lives in [`/deploy/fly`](../../deploy/fly), one `fly.toml` per app, built from the shared [`/deploy/go.Dockerfile`](../../deploy/go.Dockerfile) (or [`/deploy/web.Dockerfile`](../../deploy/web.Dockerfile) for the frontend).

Phase 1 deploys a smaller set of apps than the eventual topology above, since NATS, the event engine's rules, and Postgres aren't introduced until Phase 2 ([`implementation-plan.md`](../implementation-plan.md)):

| Fly app | Source | Public? | Notes |
|---|---|---|---|
| `sky-radar-adapter-opensky` | `cmd/adapter-opensky` | No (internal only) | Optional `OPENSKY_CLIENT_ID`/`OPENSKY_CLIENT_SECRET` via `fly secrets set` |
| `sky-radar-adapter-adsblol` | `cmd/adapter-adsblol` | No | |
| `sky-radar-adapter-airplaneslive` | `cmd/adapter-airplaneslive` | No | |
| `sky-radar-normalizer` | `cmd/normalizer` | No | Reads/writes Redis directly (no stream bus yet) |
| `sky-radar-apigateway` | `cmd/apigateway` | Yes, `https://sky-radar-apigateway.fly.dev` | `GET /flights`, `GET /flights/{icao24}` |
| `sky-radar-redis` | `redis:7-alpine` + persistent volume | No | Self-hosted per the rationale above; hot state is rebuildable from the live feed, so volume loss is not a DR event |
| `sky-radar-web` | `web/` (static, built by Vite, served by nginx) | Yes, `https://sky-radar-web.fly.dev` | `VITE_API_BASE_URL` is baked in at image build time |

`eventengine` is intentionally **not deployed** in Phase 1 — it currently only exposes `/healthz` and has no rule logic to run (see [`phase-1-foundation.md`](../prd/phase-1-foundation.md#out-of-scope-explicitly-deferred)); deploying it would be standing up infrastructure for a no-op service. It gets a Fly app once Phase 2 gives it something to consume.

All app-to-app traffic (adapters/normalizer → Redis) uses Fly's private network (`<app>.internal`), never the public internet.

### CI/CD wiring (`.github/workflows/ci.yml`)

1. **Every PR:** `backend`/`frontend` (lint, vet, unit tests, frontend build) plus `container-build`, which builds every backend service's image from `deploy/go.Dockerfile` with `push: false` — this is the "container build (no push)" step from the intended pipeline above, now real.
2. **On merge to `main`:** `container-push` builds and pushes each backend image to GHCR, tagged `:latest` and `:<sha>`.
3. **Production promotion:** `deploy-production` runs behind a `production` GitHub Environment (configure a required-reviewer protection rule on that environment for the manual approval gate). It deploys `redis` first (`flyctl deploy --config deploy/fly/redis.toml`, pulling `redis:7-alpine` directly) so dependent services have a running Redis before they start, then promotes the exact image `container-push` built for the backend services (`flyctl deploy --image ghcr.io/...:<sha>`), and builds `web` last directly via Fly's remote builder (it isn't pushed to GHCR, since its `VITE_API_BASE_URL` build arg is Fly-deployment-specific). The matrix runs with `max-parallel: 1` in this order, and a `concurrency` group serializes overlapping `deploy-production` runs across merges.

There is no separate staging tier yet — Phase 1 promotes straight to production. Standing up a parallel staging Fly org/app set is deferred until there's more to validate against it (load/chaos testing lands in Phase 3); doing it now would be infrastructure ahead of need, which cuts against this project's own stated principle (see [`implementation-plan.md`](../implementation-plan.md#cross-cutting-notes)).

### One-time setup before the first deploy

A maintainer with Fly access must, once:

1. `fly apps create` each app name listed in the table above (Fly app names are globally unique — rename in both the relevant `deploy/fly/*.toml` and this table if a name is taken).
2. `fly volumes create sky_radar_redis_data --region sjc --size 1 -a sky-radar-redis` (see [`deploy/fly/redis.toml`](../../deploy/fly/redis.toml)), then `flyctl deploy --config deploy/fly/redis.toml` once so `sky-radar-redis` is running before the first CI-driven deploy of the dependent services.
3. Add a `FLY_API_TOKEN` repository secret (`fly tokens create deploy`) and create a `production` GitHub Environment with a required-reviewer rule, so `deploy-production` has both the credential and the manual approval gate.
4. Optionally set `OPENSKY_CLIENT_ID`/`OPENSKY_CLIENT_SECRET` via `fly secrets set -a sky-radar-adapter-opensky` (the adapter runs anonymously, at a lower rate limit, without them).

Until step 3 is done, `deploy-production` will fail at the `flyctl deploy` step — `container-build`/`container-push` and the rest of CI are unaffected. After that, `deploy-production` keeps `sky-radar-redis` up to date on every merge to `main` (see the CI/CD wiring above), so step 2's manual deploy is only needed for the very first run.
