# Runbook: Phase 3 Load-Test Harness

## Purpose

[`phase-3-reliability-and-scale.md`](../prd/phase-3-reliability-and-scale.md)
(P3-FR3, P3-FR4) requires the system's capacity targets — the master
PRD's 50,000-tracked-entity headroom and the freshness/fan-out SLOs in
[`00-master-prd.md`](../prd/00-master-prd.md#8-non-functional-requirements-slos)
— to be validated with real load tests, not assumed from design alone.
This runbook covers the two harnesses under [`loadtest/`](../../loadtest/)
that produce that evidence: simulated ingest volume and simulated
viewport churn, per
[`system-architecture.md`](../architecture/system-architecture.md#scalability).

This runbook is about running the harnesses and reading their output. It
does **not** cover diagnosing or fixing whatever bottlenecks a run turns
up — that's the next phase-3 milestone
([`prompts/phase-3/02-load-tests-and-bottlenecks.md`](../../prompts/phase-3/02-load-tests-and-bottlenecks.md)).

## The two harnesses

| Harness | What it does | What it measures |
|---|---|---|
| [`loadtest/ingestvolume`](../../loadtest/ingestvolume) | Writes a configurable number of synthetic aircraft into the `raw:*` Redis keyspace, exactly the way the real adapters do today (see `internal/redisutil/rawstate.go`), with realistic per-provider payload shapes and continuously moving positions. | How long it takes each synthetic update to reach `flights.updates` (the same ingest → normalize path real traffic uses), and whether every injected aircraft is actually covered. |
| [`loadtest/viewportchurn`](../../loadtest/viewportchurn) | Opens many concurrent WebSocket connections to the real public `GET /api/v1/ws` endpoint, each with a viewport that pans/zooms on a jittered interval around a real-world traffic hotspot (NYC, LHR, LAX, FRA, SIN, DXB, HND, ATL). | Connection success rate, handshake latency, and per-message freshness as experienced by a real client — the same WS protocol, auth, and rate limiting a browser goes through. |

Both report the same freshness metric (seconds between a `FlightState`'s
`last_seen_utc` and when it's actually observed) so their output is
directly comparable to each other and to the master PRD's "Data freshness
(P95) ≤ 15s normal / >60s degraded" SLO line. Run them separately to
isolate the ingest pipeline from the WS fan-out path, or simultaneously to
exercise the whole system at once and to approximate P3-FR4's "isolate
one client's bandwidth/latency while global ingest volume is spiked"
scenario.

## Prerequisites

- Go 1.26+ (matches the module's `go.mod`).
- A running stack to point at: local `docker compose up --build`, or a
  staging deployment with Redis/NATS reachable (`ingestvolume` writes
  directly to Redis and reads directly from NATS JetStream — it is an
  operator tool, not a public client, so it needs the same network access
  the normalizer itself has) and the API gateway's public URL reachable
  (`viewportchurn` only ever talks to the public `/api/v1/ws` route).
- For `viewportchurn` runs above ~50 concurrent clients: an elevated-tier
  API key (`cmd/apigateway -issue-key`), since the anonymous tier's
  default 60 connections/min per-IP budget (`cmd/apigateway/auth.go`) will
  otherwise throttle the ramp-up itself rather than the system under test.

## Running ingestvolume

```sh
docker compose up -d --build
go run ./loadtest/ingestvolume \
  -aircraft 5000 \
  -duration 5m \
  -report /tmp/ingestvolume-report.json
```

Quick smoke run before committing to a long one:

```sh
go run ./loadtest/ingestvolume -aircraft 50 -duration 30s -grace-period 20s
```

Key flags (`go run ./loadtest/ingestvolume -h` for the full list):

- `-aircraft`: fleet size. Push toward 50,000 to test the master PRD's
  documented headroom target; start much lower to find the curve first.
- `-update-interval`: per-aircraft update cadence (default 15s, matching
  the real adapters' poll interval).
- `-multi-source-fraction`: fraction of aircraft reported by two providers
  at once, so the run exercises the normalizer's multi-source merge path,
  not just its single-source path.
- `-grace-period`: extra wait after injection stops, so in-flight merge
  cycles land before the run measures coverage/freshness. Increase this
  (or decrease `-duration`'s expectations) if you see a coverage warning.

The harness uses ICAO24 addresses in the `fff*` block, which is unused by
any real assigning authority, so it's safe to run against a staging
environment that also has real adapters live — the tail reader only
counts synthetic traffic.

## Running viewportchurn

```sh
docker compose up -d --build
go run ./loadtest/viewportchurn \
  -clients 200 \
  -ramp-up 30s \
  -duration 5m \
  -report /tmp/viewportchurn-report.json
```

Quick smoke run:

```sh
go run ./loadtest/viewportchurn -clients 10 -ramp-up 2s -duration 20s
```

Against a non-local target, override `-ws-url` (and supply `-api-key` for
larger client counts):

```sh
go run ./loadtest/viewportchurn \
  -ws-url wss://skyradar.swanathiyarath.com/api/v1/ws \
  -api-key "$SKYRADAR_API_KEY" \
  -clients 500 -ramp-up 2m -duration 10m
```

## Reading the output

Both harnesses print a one-line config banner, periodic progress logs,
and a final summary ending in a `report.PrintFreshness` line shaped like:

```text
ingest-to-flights.updates freshness: count=18234 min=0.41s p50=2.10s p95=7.84s p99=12.30s max=41.02s -> PASS
```

The verdict after `->` is:

- **PASS** — P95 is at or under the 15s freshness SLO target.
- **WARN** — P95 is above the 15s target but at or under the 60s
  degraded-mode threshold; the SLO is missed but the system would still
  be in "stale, not broken" territory in production.
- **FAIL** — P95 exceeds the 60s degraded-mode threshold.

`ingestvolume` additionally prints a coverage warning if any injected
aircraft never showed up on `flights.updates` at all — that's a stronger
signal than a high-latency PASS/WARN/FAIL, since it means the normalizer
dropped data rather than just delivering it late.

`-report <path>` writes the same numbers as JSON, for diffing across runs
or feeding into the bottleneck-fixing milestone's before/after comparison.

## What this runbook does not do

Per [`01-load-test-harness.md`](../../prompts/phase-3/01-load-test-harness.md)'s
scope: this is the harness, not a capacity report. Running it once and
recording the numbers does not satisfy P3-FR3 — that requires deliberately
pushing `-aircraft`/`-clients` up until something breaks, which is the
next milestone's job.

## Bottleneck findings (phase-3 milestone 02, ingestvolume)

Running `ingestvolume` against a local `docker compose` stack surfaced two
ingest-path bottlenecks, both fixed in this milestone. All runs below used
the same stack (Redis/NATS on one machine, real adapters also live and
polling production sources concurrently — `ingestvolume`'s synthetic
`fff*` ICAO24 block is isolated from that real traffic by design, see
above).

**1. Sequential per-aircraft Redis writes.** Every adapter (and
`ingestvolume` itself) wrote its poll batch with a plain `for` loop calling
`WriteRawState` once per aircraft, so wall-clock cost grew linearly with
batch size. Fixed by `redisutil.WriteRawStatesConcurrently`, a
semaphore-bounded (concurrency 64) fan-out, reused by all three adapters,
`ingestvolume`, and (as `mergeConcurrency`, already present) the
normalizer's publish side.

**2. Redundant republish of unchanged state.** `runMergeLoop` re-merged
and re-published every `raw:*` entry on every 15s tick regardless of
whether its underlying raw report had actually changed. Since a raw
entry's TTL (60s) outlives several merge intervals, any aircraft not
re-polled on a given cycle still got rebroadcast on `flights.updates` with
its old, increasingly stale `LastSeenUTC` — wasting NATS throughput and
corrupting freshness measurement (P95 was tracking the republish gap, not
ingest latency). Fixed by tracking the last-published `LastSeenUTC` per
ICAO24 in `persistAndPublishAll` and skipping the publish (but still
renewing the Redis hot-state TTL) when it hasn't changed.

### Before / after (same stack, same scenario, fix toggled via the working
tree's diff)

| Scenario | Metric | Before | After |
|---|---|---|---|
| 200 aircraft, 45s update interval, 90s duration (isolates the republish bug: merge interval 15s « update interval) | `flights.updates` messages | 1400 (7×/aircraft for 2 real updates) | 400 (exactly 2×/aircraft) |
| same | freshness P95 | 50.79s — WARN | 5.66s — PASS |
| 8,000 aircraft, default 15s interval (isolates the write-loop bottleneck at scale) | `flights.updates` messages | 48,000 (1.15×/aircraft excess) | 32,000 (exact) |
| same | freshness P95 | 37.14s — WARN | 8.34s — PASS |

### Capacity check against the master PRD's 50,000-entity headroom target

With both fixes applied, `ingestvolume` was pushed toward the documented
ceiling:

| Fleet size | Coverage | Freshness P95 | Verdict |
|---|---|---|---|
| 8,000 | 100% | 8.34s | PASS |
| 20,000 | 100% | 7.54s | PASS |
| 50,000 | 100% | 17.90s | WARN (within the 60s degraded threshold; above the 15s SLO) |

At the documented 50,000-entity ceiling, coverage holds at 100% (no data
loss) but P95 freshness slips just past the 15s SLO into WARN territory on
this single-machine local stack. `runMergeLoop`'s own cycle-duration logs
show merge cycles completing without backlog at every fleet size tested
(no cycle exceeded the 15s ticker interval), so this isn't the same class
of bug as the two above — it reads as the system genuinely nearing its
per-cycle processing ceiling at the edge of the stated headroom target,
not a discrete, fixable defect. Recorded here rather than chased further,
per the master PRD's "truth, not a clean scoreboard" framing — it's a
candidate for a follow-up milestone (e.g. parallelizing the JSON decode in
`ScanRawStates`, or profiling Redis/NATS round-trip cost directly) rather
than a fix this milestone's scope covers.
