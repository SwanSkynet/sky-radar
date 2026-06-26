package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
	"github.com/SwanSkynet/sky-radar/internal/pgstore"
)

// eventengineTestDSN mirrors internal/pgstore's testdb_test.go default: the
// postgres service container CI and docker-compose both run, overridable
// via TEST_DATABASE_URL for any other environment.
const eventengineTestDSN = "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"

func testEventenginePostgres(t *testing.T) *pgstore.Store {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = eventengineTestDSN
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store, err := pgstore.Connect(ctx, dsn)
	if err != nil {
		t.Skipf("postgres unavailable, skipping integration test: %v", err)
	}
	t.Cleanup(store.Close)

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return store
}

// TestRefreshZonesWatchlistPicksUpPersistedZone proves the acceptance
// criterion that a geofence created through Postgres (i.e. via the
// apigateway POST /zones endpoint in production) actually drives the
// running event engine's matching behavior, without constructing the
// engine through a restart.
func TestRefreshZonesWatchlistPicksUpPersistedZone(t *testing.T) {
	pg := testEventenginePostgres(t)
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	session := fmt.Sprintf("session-%d", time.Now().UnixNano())
	zone := flightmodel.Zone{
		ID:   flightmodel.NewID(),
		Name: "Persisted Zone",
		Polygon: flightmodel.GeoJSONPolygon{
			Type: "Polygon",
			Coordinates: [][][]float64{
				{{-122.5, 37.5}, {-122.0, 37.5}, {-122.0, 38.0}, {-122.5, 38.0}, {-122.5, 37.5}},
			},
		},
		CreatedBySession: session,
		CreatedAt:        time.Now().UTC(),
	}
	if err := pg.InsertZone(ctx, zone); err != nil {
		t.Fatalf("InsertZone: %v", err)
	}

	geofenceDetector := NewGeofenceDetector(nil)
	watchlistDetector := NewWatchlistDetector(nil)
	refreshZonesWatchlist(ctx, logger, pg, geofenceDetector, watchlistDetector, nil, nil)

	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	// First Observe against the newly-loaded zone establishes the outside
	// baseline (no event), mirroring GeofenceDetector's documented
	// first-sighting behavior.
	geofenceDetector.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", Lat: 39.0, Lon: -122.25, LastSeenUTC: now})

	events := geofenceDetector.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", Lat: 37.75, Lon: -122.25, LastSeenUTC: now.Add(10 * time.Second)})
	// ListAllZones reads every zone ever inserted by any test against this
	// shared test database (other test files reuse this same square region
	// without cleanup), so other concurrently-matching zones are expected;
	// what matters is that our specific persisted zone is among them.
	foundOurZone := false
	for _, e := range events {
		if e.Type != flightmodel.EventTypeGeofenceEnter {
			continue
		}
		var detail geofenceDetail
		if err := json.Unmarshal(e.Detail, &detail); err == nil && detail.ZoneID == zone.ID {
			foundOurZone = true
		}
	}
	if !foundOurZone {
		t.Fatalf("Observe after refresh from Postgres = %+v, want a geofence_enter event for zone %s", events, zone.ID)
	}
}

// TestRefreshZonesWatchlistPicksUpPersistedWatchlistEntry mirrors
// TestRefreshZonesWatchlistPicksUpPersistedZone for the watchlist rule.
func TestRefreshZonesWatchlistPicksUpPersistedWatchlistEntry(t *testing.T) {
	pg := testEventenginePostgres(t)
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	session := fmt.Sprintf("session-%d", time.Now().UnixNano())
	icao24 := fmt.Sprintf("t%d", time.Now().UnixNano())
	entry := flightmodel.WatchlistEntry{
		ID:               flightmodel.NewID(),
		ICAO24:           icao24,
		Label:            "Persisted Entry",
		CreatedBySession: session,
		CreatedAt:        time.Now().UTC(),
	}
	if err := pg.InsertWatchlistEntry(ctx, entry); err != nil {
		t.Fatalf("InsertWatchlistEntry: %v", err)
	}

	geofenceDetector := NewGeofenceDetector(nil)
	watchlistDetector := NewWatchlistDetector(nil)
	refreshZonesWatchlist(ctx, logger, pg, geofenceDetector, watchlistDetector, nil, nil)

	_, matched := watchlistDetector.Observe(flightmodel.FlightState{ICAO24: icao24, LastSeenUTC: time.Now().UTC()})
	if !matched {
		t.Fatal("Observe after refresh from Postgres = no match, want a match for the persisted watchlist entry")
	}
}
