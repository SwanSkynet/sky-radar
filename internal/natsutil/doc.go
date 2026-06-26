// Package natsutil provides NATS JetStream connection, stream, publish, and
// subscribe helpers shared by the normalizer (producer) and downstream
// consumers (event engine, durable-store writer, API gateway) of
// flights.updates. See docs/tech-stack/data-and-messaging.md and
// docs/architecture/system-architecture.md.
package natsutil
