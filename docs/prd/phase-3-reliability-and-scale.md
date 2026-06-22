# Phase 3 PRD: Reliability and Scale Hardening

## Goal
Prove, rather than assume, that the system behaves correctly under failure and under load — and close the security/DR gaps that a "ship the features" phase like Phase 2 deliberately deferred. This phase is what separates a working demo from something that can credibly be called production-grade.

## Prerequisite
Phase 2 complete: events, replay, public API, and observability are all live and have been running in production long enough to have a real traffic/behavior baseline to test against.

## In scope
- **Chaos testing**: scripted failure injection — kill a source adapter, kill the NATS process, kill a Redis/Postgres connection — verifying the failure-isolation claims in [`../architecture/system-architecture.md`](../architecture/system-architecture.md#failure-isolation) actually hold.
- **Load testing**: simulated viewport churn (many concurrent WebSocket clients with realistic pan/zoom patterns) and simulated ingest volume, validated against the capacity targets in [`../architecture/system-architecture.md`](../architecture/system-architecture.md#scalability).
- **Fail-soft UI**: degraded-mode banner and per-aircraft staleness indicators, wired to the real freshness metric (this was stubbed conceptually in Phase 2; this phase makes it correct under actual degraded conditions, not just a fixed test case).
- **Disaster recovery drill**: an actual, documented, timed restore of the durable store and a redeploy of all services from infra-as-code, measured against the RPO/RTO targets in the master PRD.
- **Security review**: threat model walkthrough against [the security section of the master PRD](../prd/00-master-prd.md), dependency/container vulnerability scan gating in CI (if not already enforced), abuse-mitigation verification (rate limiting actually holds under a scripted abuse pattern), `SECURITY.md` published.
- **Backpressure policy verification**: confirm the stream bus and event engine's documented backpressure/dropping behavior under sustained overload, rather than assuming it from configuration alone.

## Out of scope
- New product features — this phase is exclusively about proving existing behavior, not adding scope. Any feature idea that comes up here goes into the backlog for after Phase 4, via the RFC process.

## Functional / verification requirements
| ID | Requirement | Acceptance criteria |
|---|---|---|
| P3-FR1 | Killing one source adapter does not affect ingestion from the other two | Automated chaos test in CI/staging, run on a schedule, alerting on regression |
| P3-FR2 | Killing the NATS process does not crash dependent services; they recover automatically on restart | Chaos test + manual verification of recovery time |
| P3-FR3 | System sustains the documented capacity target (50,000 tracked entities headroom) without breaching latency SLOs | Load test report with concrete numbers, compared against the targets in the master PRD |
| P3-FR4 | WebSocket fan-out cost for an individual client is unaffected by global load spikes | Load test specifically isolating one client's bandwidth/latency while global ingest volume is artificially spiked |
| P3-FR5 | Degraded-mode banner appears within the documented threshold when overall freshness genuinely degrades | Test by deliberately throttling/blocking a provider in staging and observing the UI |
| P3-FR6 | DR drill restores service within the RTO target from a real (not assumed) backup | Drill is executed, timed, and the result (pass/fail, actual time) is recorded in a runbook |
| P3-FR7 | No critical/high vulnerabilities in production dependencies at drill time | CI scan report attached to the phase sign-off |
| P3-FR8 | Rate limiting holds under a scripted burst significantly above the documented per-key limit | Scripted abuse test; verify `429`s returned, no service degradation for other clients |

## Definition of done
- A written report (becomes the first entry under `docs/postmortems/` or a dedicated `docs/reliability/` report) covering: chaos test results, load test results and the actual capacity ceiling observed, the DR drill outcome with real numbers, and the security review findings with remediation status.
- Every SLO in the master PRD has either been met, or has an honestly published "not yet met, here's why and what's next" note on the status page — the goal of this phase is truth, not a clean scoreboard.
