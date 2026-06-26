# Runbook: Phase 1 24-Hour Soak Test

## Purpose

[`phase-1-foundation.md`](../prd/phase-1-foundation.md) and the
[implementation plan](../implementation-plan.md#phase-1--foundation) gate
Phase 1 on the system running **unattended for 24+ hours with no manual
restarts**. This runbook is how to actually run and judge that test, either
against the local `docker-compose` stack or the production DigitalOcean
deployment.

## Prerequisites

- `curl` and `bash` (the soak script has no other dependencies).
- For a local run: `docker compose` (see [`docker-compose.yml`](../../docker-compose.yml)).
- For a production run: the public unified domain from
  [`hosting-and-deployment.md`](../tech-stack/hosting-and-deployment.md)
  (`https://skyradar.swanathiyarath.com/api`); the individual service ports are
  internal-only on the Docker network, so they can't be polled directly
  from outside — see "Production run" below for the workaround.

## What the script checks

[`scripts/soak-test.sh`](../../scripts/soak-test.sh) polls every Phase 1
service's health endpoint plus the public `GET /flights` route on a fixed
interval (default: every 60s, for 24h), and logs every check. It tracks,
per target: total checks, failures, uptime %, and the longest unbroken
outage.

Each service exposes two endpoints (see `internal/health`):

- `/healthz` — pure process liveness, always `200` once the process is
  serving HTTP. This is what the `apigateway-healthz` line in the script
  uses, so a transient Redis blip doesn't restart-loop an otherwise-healthy
  process.
- `/readyz` — pings Redis and returns `503` if it's unreachable. The
  script's `normalizer-readyz`/`adapter-*-readyz` lines hit this, so a
  real Redis outage during the soak window still shows up as `DOWN`
  lines per [P1-FR7](../prd/phase-1-foundation.md#functional-requirements)
  instead of being silently masked.

The `apigateway-flights` line also fails (not just `DOWN` on non-200) if
`GET /flights` returns `200` with an empty aircraft set, or with the same
aircraft set on every non-empty check across the run — either case means
data isn't actually flowing end-to-end even though the process is up.

The script cannot observe whether a human manually restarted a container
during the window — that half of the "no manual restarts" criterion is on
the operator's honor system. Don't restart anything by hand during the run.

Locally, a `DOWN` on `/readyz` from a transient Redis blip does **not**
require a restart: the handler pings Redis fresh on every request, so it
reports `200` again on its own as soon as Redis recovers, and the script's
`RECOVERED` line captures that. `restart: unless-stopped` only comes into
play if the process itself exits — which only happens here if the initial
Redis connectivity check at startup fails (see each service's `main()`),
not from a steady-state outage after the process is already running.
Production behaves the same way: `docker-compose.prod.yml` has no
`HEALTHCHECK`/restart policy tied to `/healthz` or `/readyz` for the Go
services, so a Redis blip alone won't trigger a restart there either.

## Local run (docker-compose)

```sh
docker compose up -d --build
./scripts/soak-test.sh                       # 24h, 60s interval, default ports
```

To do a quick smoke check before committing to the full 24h window:

```sh
./scripts/soak-test.sh --once
```

Override duration/interval for a shorter dry run:

```sh
SOAK_DURATION_SECONDS=3600 SOAK_INTERVAL_SECONDS=30 ./scripts/soak-test.sh
```

## Production run (DigitalOcean)

Since only the API Gateway and frontend are exposed publicly through Caddy (see the topology in [`hosting-and-deployment.md`](../tech-stack/hosting-and-deployment.md)), only the public API endpoints are checkable from outside the droplet. Run the script with the targets pointing to the public unified domain:

```sh
APIGATEWAY_URL=https://skyradar.swanathiyarath.com/api \
NORMALIZER_URL=https://skyradar.swanathiyarath.com/api \
ADAPTER_OPENSKY_URL=https://skyradar.swanathiyarath.com/api \
ADAPTER_ADSBLOL_URL=https://skyradar.swanathiyarath.com/api \
ADAPTER_AIRPLANESLIVE_URL=https://skyradar.swanathiyarath.com/api \
./scripts/soak-test.sh
```

(Pointing the unreachable targets at the apigateway URL too just keeps the
script's uptime math meaningful instead of permanently red on targets it
was never going to be able to reach; the apigateway-healthz and
apigateway-flights lines are the ones that matter here.)

In parallel, SSH into the Droplet and use Docker Compose and Docker CLI tools to check internal service health and container status:

```sh
cd /root/sky-radar/deploy
docker compose -f docker-compose.prod.yml ps             # Quick health check of running containers
docker compose -f docker-compose.prod.yml ps -q | xargs docker inspect --format '{{.Name}}: {{.State.RestartCount}} restarts' # Inspect restart counts
docker compose -f docker-compose.prod.yml logs --tail=50  # Inspects logs from the services
```

Use `docker compose ps` as a quick health check of current container states and uptime. For a definitive "no manual restarts" signal, check the container restart count via `docker inspect` (as `ps` only shows the current status and can hide transient restarts). A restart count of `0` is the authoritative signal that no unexpected restarts occurred: any restart event in that window needs an explanation.

## Checking P1-FR2 (no sustained rate-limiting)

[P1-FR2](../prd/phase-1-foundation.md#functional-requirements) requires
"zero sustained `429` responses" from each adapter. Adapter errors are
logged as structured JSON with the literal status code in the message
(e.g. `"opensky: status 429"`), so grep for it after the run:

```sh
# Local
docker compose logs adapter-opensky adapter-adsblol adapter-airplaneslive | grep '"err":"[a-z.]*: status 429"'

# Production (SSH'ed into the Droplet)
cd /root/sky-radar/deploy
docker compose -f docker-compose.prod.yml logs --since 24h | grep 'status 429'
```

A handful of isolated `429`s is expected (the backoff in
[`internal/sourceadapter/backoff.go`](../../internal/sourceadapter/backoff.go)
exists to handle exactly that). What fails the criterion is a *sustained*
run of them — i.e. the adapter logging `429`s back-to-back for many
consecutive poll cycles instead of recovering within a few retries.

## Pass / fail

The soak test passes when, over the full 24h+ window:

- The soak script's summary shows 0 failures on every target it could
  reach (or: any `DOWN` windows are brief, self-recovered, and explained —
  e.g. a single missed poll during a production deploy, not an outage).
- The `docker inspect` restart-count check from "Production run" above
  shows `0` restarts for every container, or any restart is explained by
  an automatic recovery from a real transient failure, not a human
  intervening.
- The P1-FR2 grep above shows no sustained `429` runs for any adapter.
- The `apigateway-flights` line in the summary shows `ever_non_empty=true`
  and `ever_changed=true` — the script already fails this on its own if
  `GET /flights` returned an empty or unchanging aircraft set, confirming
  data is actually flowing end-to-end and not just that the process is up.

If any of the above fails, fix the root cause and restart the 24h clock —
per the [implementation plan](../implementation-plan.md#phase-1--foundation),
this milestone is "calendar-blocking": Phase 1 isn't done until a full
unattended run passes.
