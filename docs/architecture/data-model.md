# Data Model

This is the authoritative schema reference. The Go types in `/internal/flightmodel` (see [`../tech-stack/backend.md`](../tech-stack/backend.md)) and the published OpenAPI/GraphQL schema must match this document — schema drift is caught by contract tests in CI.

## FlightState (canonical, in-memory + wire format)
```
FlightState {
  icao24: string             // primary key, hex transponder address, lowercase
  callsign: string | null
  registration: string | null
  lat: float
  lon: float
  altitude_baro_ft: int | null
  altitude_geo_ft: int | null
  ground_speed_kt: float | null
  vertical_rate_fpm: float | null
  heading_deg: float | null
  on_ground: bool
  squawk: string | null
  sources: string[]          // which providers currently report this aircraft, e.g. ["opensky", "adsblol"]
  position_quality: enum     // "adsb" | "mlat" | "estimated" — derived, see merge rule below
  last_seen_utc: timestamp
  stale: bool                // derived: now - last_seen_utc > staleness threshold (see master PRD SLOs)
}
```

**Multi-source merge precedence (owned by the normalizer, see [`system-architecture.md`](system-architecture.md)):**
1. Prefer the report with the most recent `last_seen_utc`.
2. On a near-tie (within the same polling interval), prefer position quality `adsb` over `mlat` over `estimated`.
3. `sources` always lists every provider currently reporting that `icao24`, regardless of which one "won" the merge — this is what lets the frontend show "multi-source confirmed" vs. "single-source" to the user.

**Provider field mapping (for adapter implementers):** see the per-provider field tables in [`../api-docs/`](../api-docs/README.md) — e.g., airplanes.live's `gs`/`flight`/`alt_baro` map to `ground_speed_kt`/`callsign`/`altitude_baro_ft`; OpenSky's positional array fields map by index per its documented state-vector order.

## Event (durable, Postgres `events` table + wire format)
```
Event {
  id: uuid
  type: enum (altitude_delta, speed_delta, stale_signal, geofence_enter, geofence_exit, watchlist_match)
  icao24: string
  severity: enum (info, notable, warning)
  occurred_at_utc: timestamp
  detail: json               // type-specific payload, e.g. { "delta_ft": 3200 } or { "zone_id": "..." }
}
```

## Zone (durable, Postgres `zones` table)
```
Zone {
  id: uuid
  name: string
  polygon: geojson            // native PostGIS geometry if the host supports it; GeoJSON column otherwise — see data-and-messaging.md
  created_by_session: string  // anonymous/session-scoped identifier, no account system required
  created_at: timestamp
}
```

## WatchlistEntry (durable, Postgres `watchlist_entries` table)
```
WatchlistEntry {
  id: uuid
  icao24: string              // aircraft tracked by this entry
  label: string               // user-facing name, e.g. "Friend's flight"
  created_by_session: string  // anonymous/session-scoped identifier, mirrors Zone
  created_at: timestamp
}
```
A `watchlist_match` Event fires the first time the event engine observes a `FlightState` for `icao24` after it is added to the watchlist (or after the aircraft has gone silent and reappears) — one notification per continuous period in view, not one per update.

## flight_history (durable, Postgres, downsampled)
```sql
flight_history (
  icao24, recorded_at, lat, lon,
  altitude_baro_ft, ground_speed_kt, heading_deg, on_ground
)
-- one row per aircraft per ~10s interval, see data-and-messaging.md for why downsampled
-- PRIMARY KEY (icao24, recorded_at), time-partitioned for bounded retention cost
```

## Redis key layout
| Key pattern | Type | Purpose |
|---|---|---|
| `flight:{icao24}` | Hash | Current `FlightState` fields, TTL-expired when no longer tracked |
| `flights:geo` | Geo set | `icao24` → `(lon, lat)` for `GEOSEARCH` bbox/radius queries |
| `cache:{query-hash}` | String (JSON) | Short-TTL cached API response |
| `ratelimit:{api-key}` | String/counter | Token-bucket rate-limit state |

## NATS payloads
`ingest.raw.<provider>` carries the provider's raw payload shape (not normalized) — see each provider's doc in [`../api-docs/`](../api-docs/README.md). `flights.updates` and `events.detected` carry the canonical `FlightState` and `Event` shapes above, serialized as JSON (kept human-debuggable; revisit to a binary format only if profiling shows serialization cost is actually a bottleneck — don't pre-optimize this).

## Versioning rule
Any change to `FlightState`, `Event`, or `Zone` that isn't purely additive (renaming/removing a field, changing a type) is a breaking change to the public API and follows the deprecation policy in the [master PRD](../prd/00-master-prd.md) — it cannot be made silently inside a patch release.
