# Sky Radar Documentation

This is the documentation hub. Start here, then follow the links relevant to what you're doing.

## Product requirements
- [Master PRD](prd/00-master-prd.md) — overview, goals/non-goals, users, SLOs, success metrics, roadmap, risks.
- [Phase 1 — Foundation](prd/phase-1-foundation.md)
- [Phase 2 — Real-time systems depth](prd/phase-2-realtime-systems.md)
- [Phase 3 — Reliability and scale hardening](prd/phase-3-reliability-and-scale.md)
- [Phase 4 — Packaging and governance](prd/phase-4-packaging-and-governance.md)

## Tech stack and decisions
- [Tech stack overview](tech-stack/overview.md) — the full stack at a glance, with rationale and links.
- [Backend](tech-stack/backend.md) · [Frontend](tech-stack/frontend.md) · [Data and messaging](tech-stack/data-and-messaging.md) · [Observability and ops](tech-stack/observability-and-ops.md) · [Hosting and deployment](tech-stack/hosting-and-deployment.md)
- [Architecture decision records](decisions/) — why each major technology was chosen, including rejected alternatives.

## Architecture
- [System architecture](architecture/system-architecture.md) — component responsibilities, data flow, scalability, failure isolation.
- [Data model](architecture/data-model.md) — canonical schemas (`FlightState`, `Event`, `Zone`), Redis/Postgres/NATS layouts.

## Build sequencing
- [Implementation plan](implementation-plan.md) — task-by-task build order within and across phases.

## External data sources
- [Provider comparison and field mapping](api-docs/README.md)
- [OpenSky Network](api-docs/opensky-api-docs.md) · [adsb.lol](api-docs/adsb-lol-api-docs.md) · [airplanes.live](api-docs/airplanes-live-docs.md)

## How these documents relate
```
Master PRD (what, why, SLOs)
   |
   +-- Phase PRDs (detailed requirements + acceptance criteria, per delivery phase)
   |
   +-- Tech stack docs (what's used) --> ADRs (why it was chosen over alternatives)
   |
   +-- Architecture docs (how it fits together, concretely)
   |
   +-- Implementation plan (build order)
```
If you're deciding *whether* to build something, start at the Master PRD. If you're deciding *how* to build something already in scope, start at the relevant tech-stack doc or architecture doc. If you're picking up work, start at the implementation plan.
