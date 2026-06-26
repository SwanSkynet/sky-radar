# Backend Stack

See [ADR-0001](../decisions/0001-backend-language-go.md) for why Go was chosen.

## Target repo layout (monorepo)

```
/cmd
  /adapter-opensky          # source adapter: OpenSky Network
  /adapter-adsblol          # source adapter: adsb.lol
  /adapter-airplaneslive    # source adapter: airplanes.live
  /normalizer               # merges adapter output into canonical FlightState
  /eventengine              # consumes canonical updates, emits Events
  /apigateway               # REST + GraphQL + WebSocket
/internal
  /flightmodel              # canonical FlightState / Event / Zone types
  /sourceadapter             # common adapter interface + shared polling/backoff helpers
  /natsutil                 # NATS JetStream connection/subject helpers
  /redisutil                # Redis client wrapper (hot state + cache)
  /pgstore                  # Postgres access layer (history, events, zones)
  /pgstorewriter            # durable-store writer for history/event persistence
  /geo                      # bbox/radius/point-in-polygon helpers (used where Redis/PostGIS aren't a fit)
/web                        # frontend (see frontend.md)
/deploy                     # Dockerfiles, fly.toml, docker-compose.yml for local dev
/docs
```

Each `/cmd` entry is intended to become an independently deployable binary/container; they share code only through `/internal`, never through direct imports of each other — this is what will make the bulkhead isolation described in [`system-architecture.md`](../architecture/system-architecture.md) actually hold.

## Source adapters

- Implement the common interface in `/internal/sourceadapter`: `Poll(ctx) ([]RawState, error)` plus adapter-specific auth/config.
- Each adapter owns its own polling cadence, jitter, and exponential backoff on `429`/`5xx`, tuned to that provider's documented limits (see [`docs/api-docs/`](../api-docs/README.md)).
- Adapters publish raw (pre-normalization) payloads to their own NATS subject (`ingest.raw.<provider>`) and do nothing else — no merge logic, no business rules. This keeps the failure/blast radius of a single provider's quirks contained to its own adapter.

## Normalization layer

- Single consumer of all `ingest.raw.*` subjects.
- Converts provider-specific payloads into the canonical `FlightState` schema (see [`../architecture/data-model.md`](../architecture/data-model.md)).
- Owns the dedup/merge precedence rule across providers reporting the same `icao24` (freshest timestamp wins; ties broken by position-quality: ADS-B > MLAT > estimated).
- Publishes merged updates to `flights.updates` and writes current state into Redis.

## Event engine

- Stateless consumer of `flights.updates`; evaluates each update against the rule set (altitude/speed delta thresholds, stale-signal detection, geofence enter/exit, watchlist match).
- Emits `Event` records to `events.detected`.
- Rule thresholds are configuration, not hardcoded, so they can be tuned without a redeploy of the engine binary itself (see the implementation plan for where this lands).

## Postgres writer

- Stateless consumer of `flights.updates` and `events.detected`.
- Writes downsampled `flight_history` rows and durable `events` records to Postgres.
- Keeps durable persistence separate from event evaluation so each concern can scale and fail independently.

## API gateway

- **HTTP router:** `chi` (lightweight, idiomatic, stdlib-compatible) for REST.
- **GraphQL:** `gqlgen` (schema-first, generates type-safe resolvers matching the canonical Go types in `/internal/flightmodel`).
- **WebSocket:** `github.com/coder/websocket` (modern, context-aware, no legacy baggage).
- Subscribes to `flights.updates` once per gateway instance and fans out to connected WebSocket clients, filtering server-side by each client's registered viewport bbox — this is where the "bounded per-connection bandwidth regardless of global traffic" requirement is actually implemented.
- Owns API-key auth, per-key rate limiting (token bucket, in Redis so it works across multiple gateway instances), and response caching for REST/GraphQL reads.
- The public REST surface is versioned at `/api/v1` and its OpenAPI schema is published at `GET /api/v1/openapi.yaml`, served from `cmd/apigateway/openapi-v1.yaml` (embedded into the binary) and exercised by contract tests in `cmd/apigateway/contract_test.go`. `openapi-v1.yaml` is an OpenAPI projection of the canonical schema — [`../architecture/data-model.md`](../architecture/data-model.md) remains the authoritative reference, including its "API authentication & rate limiting" section for the tier/auth model. GraphQL schema publication is deferred to a follow-up milestone — no GraphQL layer exists yet.

## Testing and tooling

- `go test` for unit tests; provider adapter contract tests run against recorded fixture payloads (captured from real responses) so CI doesn't depend on live provider calls.
- `golangci-lint` for linting, `govulncheck` for dependency vulnerability scanning — both run in CI on every PR.
- Structured logging via `log/slog` (standard library); every log line includes a trace ID for correlation with OpenTelemetry traces (see [`observability-and-ops.md`](observability-and-ops.md)).
