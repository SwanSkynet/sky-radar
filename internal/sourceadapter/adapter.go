package sourceadapter

import (
	"context"
	"encoding/json"
	"time"
)

// RawState is a single pre-normalization aircraft report as fetched from
// one provider. Adapters do not interpret, map, or merge this payload —
// the normalizer is the sole owner of converting Payload's provider-
// specific fields onto the canonical FlightState schema (see
// docs/architecture/data-model.md and docs/tech-stack/backend.md).
type RawState struct {
	Provider  string          `json:"provider"`
	ICAO24    string          `json:"icao24"`
	FetchedAt time.Time       `json:"fetched_at"`
	Payload   json.RawMessage `json:"payload"`
}

// Adapter is the common interface every provider source adapter
// implements. See docs/tech-stack/backend.md.
type Adapter interface {
	Poll(ctx context.Context) ([]RawState, error)
}
