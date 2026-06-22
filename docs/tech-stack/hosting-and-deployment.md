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
