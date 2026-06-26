// Package pgstorewriter persists durable history/event records to
// Postgres. It consumes flights.updates (downsampled into flight_history
// rows) and events.detected (written through as events rows), kept
// deliberately separate from event evaluation (cmd/eventengine) so
// persistence and rule evaluation can scale and fail independently. See
// docs/tech-stack/backend.md's "Postgres writer" section.
//
// HistoryWriter keeps its per-aircraft downsampling bookkeeping
// (lastSeen) in process memory rather than Redis, so it is hot state
// local to a single process instance: it resets on restart and is not
// shared across replicas of this service.
package pgstorewriter
