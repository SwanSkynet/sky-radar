# ADR-0003: Stream bus is NATS JetStream

## Status
Accepted

## Context
Sky Radar needs a decoupling layer between ingestion (source adapters → normalization) and consumers (event engine, durable-store writer, WebSocket broadcaster), so that adding a new consumer never requires touching ingestion code, and so recent history can be replayed. Candidates considered: Apache Kafka, Redis Streams, NATS JetStream.

## Decision
The stream bus is NATS JetStream.

## Rationale
- **Operational weight matches [ADR-0002](0002-hosting-lightweight-containers.md).** NATS (with JetStream enabled) ships as a single small binary with no external coordination service required. Kafka's equivalent footprint (even with KRaft replacing ZooKeeper) is significantly heavier in memory and operational surface — a mismatch for a free-tier-hosted, volunteer-run system.
- **It does what's actually needed, no more.** JetStream provides persistence, at-least-once delivery, and replay (consumers can re-read from a point in time) — exactly the properties Sky Radar's replay feature and durable-store writer need — without Kafka's partition/broker/consumer-group machinery that's built for a scale Sky Radar isn't operating at.
- **Avoids overloading a single system.** Redis Streams would mean Redis (already serving as the hot state store/cache — see [`data-and-messaging.md`](../tech-stack/data-and-messaging.md)) also carries the stream-bus responsibility. Keeping the stream bus separate from the hot store means a NATS slowdown/restart doesn't take live-map queries down with it, and vice versa.
- **Native subject-based filtering** is a good match for the system's actual fan-out shape (see [`system-architecture.md`](../architecture/system-architecture.md)): adapters publish to per-provider subjects, the normalizer publishes canonical updates to a single subject, and any number of independent consumers subscribe without coordinating with each other.

## Rejected alternatives
- **Apache Kafka** — the strongest individual "I've operated distributed systems" signal, but its operational footprint (broker memory, storage management, partition rebalancing) is disproportionate to Sky Radar's actual throughput (tens of thousands of updates, not millions of events/sec) and incompatible with the cost/ops constraints in ADR-0002.
- **Redis Streams** — would avoid introducing a new system, but has materially less mature replay/consumer-group semantics than NATS or Kafka, and conflates the stream-bus and hot-state-cache responsibilities in one process, removing a useful failure boundary.

## Consequences
- NATS subject naming convention is defined in [`system-architecture.md`](../architecture/system-architecture.md#stream-bus-subjects) and must be followed by any new adapter or consumer.
- Geospatial/viewport filtering happens in-process at the API gateway (subscribing to the canonical-updates subject and filtering per connected client), not via subject-sharding by region — deliberately avoiding premature complexity at Sky Radar's target scale.
- If Sky Radar's throughput or team size grows well past the scale assumptions in the PRD, this decision is revisitable via the RFC process, not locked in permanently.
