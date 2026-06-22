# Data and Messaging Stack

Covers NATS JetStream (stream bus), Redis (hot state + cache), and PostgreSQL (durable store). See [ADR-0003](../decisions/0003-stream-bus-nats-jetstream.md) for why NATS was chosen over Kafka/Redis Streams.

## NATS JetStream — stream bus
**Role:** decouples ingestion from every consumer; provides persistence and replay.

**Subject naming convention:**
| Subject | Published by | Consumed by | Payload |
|---|---|---|---|
| `ingest.raw.opensky` | OpenSky adapter | Normalizer | Raw OpenSky state vector |
| `ingest.raw.adsblol` | adsb.lol adapter | Normalizer | Raw adsb.lol aircraft object |
| `ingest.raw.airplaneslive` | airplanes.live adapter | Normalizer | Raw airplanes.live aircraft object |
| `flights.updates` | Normalizer | Event engine, durable-store writer, API gateway (WS broadcaster) | Canonical `FlightState` |
| `events.detected` | Event engine | Durable-store writer, API gateway (for in-app notifications) | `Event` |

**Stream/retention config:** `ingest.raw.*` streams retain a short window (minutes) — they only need to survive a normalizer restart. `flights.updates` and `events.detected` retain the full replay window (default 30 minutes full-resolution) backing the frontend's replay feature (see [phase-2-realtime-systems.md](../prd/phase-2-realtime-systems.md)).

**Deliberate simplicity:** there is no subject-sharding by geographic region. At Sky Radar's target scale (tens of thousands of aircraft), a single consumer can filter in-process; sharding by region would add coordination complexity for a problem that doesn't exist yet at this scale. Revisit only if a future capacity review shows this is actually a bottleneck.

## Redis — hot state and cache
**Role:** "current state of the world" for live map queries, plus short-TTL response caching.

- `flight:{icao24}` — Hash of the current `FlightState` fields for one aircraft, written by the normalizer on every update, with a TTL slightly longer than the stale-data threshold (after which absence from Redis itself signals "no longer tracked," distinct from the `stale: true` flag which signals "tracked but not recently updated").
- `flights:geo` — Geo set (`GEOADD`) of `icao24` → `(lon, lat)`, enabling `GEOSEARCH` for bounding-box/radius queries without building custom spatial indexing in Go.
- `cache:*` — separate keyspace for short-TTL cached API responses (viewport queries, flight detail lookups), TTL tuned to the freshness SLO so cached responses are never staler than what the freshness SLO already tolerates.
- `ratelimit:*` — token-bucket counters for per-API-key rate limiting, shared across API gateway instances.

## PostgreSQL — durable store
**Role:** event history and a downsampled position history backing replay beyond Redis's hot window, plus zones.

**Why downsampled, not every update:** writing every single normalized update (potentially every 1-5s per aircraft, tens of thousands of aircraft) to Postgres would be an unnecessary write-amplification and storage cost for a project with a near-zero budget. The durable-store writer batches and downsamples to one row per aircraft per ~10s interval for `flight_history`; full-resolution recent data lives only in Redis/JetStream's retention window. This is a deliberate cost/fidelity trade-off, not an oversight — document it as such if asked why replay beyond ~30 minutes is lower-resolution.

**Representative schema (see [`../architecture/data-model.md`](../architecture/data-model.md) for the full, authoritative version):**
```sql
CREATE TABLE flight_history (
  icao24 text NOT NULL,
  recorded_at timestamptz NOT NULL,
  lat double precision,
  lon double precision,
  altitude_baro_ft integer,
  ground_speed_kt real,
  heading_deg real,
  on_ground boolean,
  PRIMARY KEY (icao24, recorded_at)
);

CREATE TABLE events (
  id uuid PRIMARY KEY,
  type text NOT NULL,
  icao24 text NOT NULL,
  severity text NOT NULL,
  occurred_at timestamptz NOT NULL,
  detail jsonb
);

CREATE TABLE zones (
  id uuid PRIMARY KEY,
  name text NOT NULL,
  polygon jsonb NOT NULL,        -- GeoJSON; see note below on PostGIS
  created_by_session text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
```

**PostGIS note:** if the chosen Postgres host supports the PostGIS extension, `zones.polygon` should be a native `geometry` column and geofence enter/exit checks should use `ST_Contains`. If the host is a vanilla managed Postgres without extensions (common on free tiers), polygons are stored as GeoJSON and point-in-polygon checks run in the event engine using a Go geometry library (`github.com/paulmach/orb`). This decision is host-dependent and finalized in [`hosting-and-deployment.md`](hosting-and-deployment.md) once a specific provider is chosen.

**Partitioning:** `flight_history` is time-partitioned (by day or week) so old partitions can be dropped or downsampled-further on a retention schedule without expensive `DELETE` queries — necessary for keeping storage cost bounded over time on a free/cheap-tier database.
