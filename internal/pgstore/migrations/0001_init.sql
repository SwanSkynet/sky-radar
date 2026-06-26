-- Durable store schema for Phase 2, matching the authoritative tables in
-- docs/architecture/data-model.md and docs/tech-stack/data-and-messaging.md.

-- flight_history is range-partitioned on recorded_at per
-- docs/tech-stack/data-and-messaging.md ("Partitioning"), so old data can
-- later be dropped/downsampled-further per-partition without an expensive
-- DELETE. A single DEFAULT partition is created here to keep this
-- migration self-contained; rolling per-day/week partitions and retiring
-- the default one is ops automation (Phase 3 territory), not this
-- milestone's job.
CREATE TABLE IF NOT EXISTS flight_history (
  icao24 text NOT NULL,
  recorded_at timestamptz NOT NULL,
  lat double precision NOT NULL,
  lon double precision NOT NULL,
  altitude_baro_ft integer,
  ground_speed_kt real,
  heading_deg real,
  on_ground boolean NOT NULL,
  PRIMARY KEY (icao24, recorded_at)
) PARTITION BY RANGE (recorded_at);

CREATE TABLE IF NOT EXISTS flight_history_default PARTITION OF flight_history DEFAULT;

CREATE TABLE IF NOT EXISTS events (
  id uuid PRIMARY KEY,
  type text NOT NULL,
  icao24 text NOT NULL,
  severity text NOT NULL,
  occurred_at_utc timestamptz NOT NULL,
  detail jsonb
);

CREATE INDEX IF NOT EXISTS events_icao24_occurred_at_idx ON events (icao24, occurred_at_utc DESC);
CREATE INDEX IF NOT EXISTS events_occurred_at_idx ON events (occurred_at_utc DESC);

-- zones and watchlist_entries are part of the documented durable-store
-- schema but neither is written by the flights.updates/events.detected
-- consumer this milestone builds (see docs/tech-stack/backend.md's
-- "Postgres writer" boundary) — their write paths land with the
-- zones/watchlist feature work itself.
CREATE TABLE IF NOT EXISTS zones (
  id uuid PRIMARY KEY,
  name text NOT NULL,
  polygon jsonb NOT NULL,
  created_by_session text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS watchlist_entries (
  id uuid PRIMARY KEY,
  icao24 text NOT NULL,
  label text NOT NULL,
  created_by_session text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
