// Package pgstore provides Postgres access for history, events, and zones:
// connection management, schema migrations, and the insert paths the
// durable-store writer (internal/pgstorewriter) uses. See
// docs/tech-stack/data-and-messaging.md and docs/architecture/data-model.md
// for the authoritative schema.
package pgstore
