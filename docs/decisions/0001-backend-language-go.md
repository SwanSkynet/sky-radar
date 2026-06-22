# ADR-0001: Backend services are written in Go

## Status
Accepted

## Context
Sky Radar's backend has four kinds of long-running services: per-provider source adapters (continuous polling), a normalization/dedup layer, an event engine (stream consumer), and an API gateway (REST/GraphQL/WebSocket with potentially thousands of concurrent viewport subscriptions). All of this runs continuously, unattended, on free/low-cost infrastructure operated by volunteers. Candidates considered: Go, TypeScript/Node.js, Rust, Python.

## Decision
Backend services are written in Go.

## Rationale
- **Concurrency fits the workload directly.** Goroutines make "poll N providers concurrently, fan out M aircraft updates to K WebSocket subscribers" cheap to write and cheap to run, without an async-runtime learning curve (vs. Node) or borrow-checker overhead for this kind of I/O-bound code (vs. Rust).
- **Resource footprint matters because hosting is free/cheap-tier.** A Go binary's memory footprint and startup time are small relative to a Node process running the same workload, which directly affects how many services fit on a small VPS/PaaS instance within budget.
- **Single static binary deploys** simplify the lightweight, no-Kubernetes hosting model (see [ADR-0002](0002-hosting-lightweight-containers.md)) — a service is one artifact, easy to containerize minimally (e.g., `FROM scratch` or `distroless`).
- **Observability ecosystem is native territory.** Prometheus, the de facto metrics standard this project relies on (see [`observability-and-ops.md`](../tech-stack/observability-and-ops.md)), is itself written in Go, and Go's client libraries and conventions for metrics/tracing are first-class, not bolted on.
- **Contributor accessibility is acceptable.** Go's syntax and standard library are small and explicit enough that contributors coming from any other mainstream language can read and modify adapter code without deep Go expertise — a meaningfully lower bar than Rust.

## Rejected alternatives
- **TypeScript/Node.js** — would let frontend and backend share a language, but Node's higher memory-per-connection cost and weaker raw-concurrency story are a worse fit for a 24/7 multi-provider ingestion + high-fanout WebSocket service running on a memory-constrained host.
- **Rust** — better raw performance and memory safety, but materially steeper learning curve; for an open-source project depending on outside contributions, this tax outweighs the performance benefit at Sky Radar's actual scale (tens of thousands of aircraft, not millions).
- **Python** — fastest to prototype and has the richest aviation/data tooling, but its concurrency model (GIL, async bolted onto a sync-first ecosystem) and higher per-process resource use make it the weakest fit for a long-running, concurrency-heavy, cost-constrained service.

## Consequences
- All backend services live under `/cmd` and `/internal` in the monorepo (see [`backend.md`](../tech-stack/backend.md)).
- CI tooling (lint, test, vuln scanning) is Go-centric (`golangci-lint`, `go test`, `govulncheck`).
- Frontend and backend are different languages, so shared types (e.g., `FlightState`) are kept in sync via the published OpenAPI/GraphQL schema rather than shared source code.
