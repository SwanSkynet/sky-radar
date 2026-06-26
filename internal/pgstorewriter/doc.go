// Package pgstorewriter persists durable history/event records to
// Postgres. It is a stateless consumer of flights.updates (downsampled
// into flight_history rows) and events.detected (written through as
// events rows), kept deliberately separate from event evaluation
// (cmd/eventengine) so persistence and rule evaluation can scale and fail
// independently. See docs/tech-stack/backend.md's "Postgres writer"
// section.
package pgstorewriter
