package redisutil

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
	"github.com/SwanSkynet/sky-radar/internal/geo"
	"github.com/redis/go-redis/v9"
)

func sampleFlightState(icao24 string, lat, lon float64) flightmodel.FlightState {
	callsign := "UAL123"
	altBaro := 35000
	gs := 420.5
	return flightmodel.FlightState{
		ICAO24:          icao24,
		Callsign:        &callsign,
		Lat:             lat,
		Lon:             lon,
		AltitudeBaroFt:  &altBaro,
		GroundSpeedKt:   &gs,
		OnGround:        false,
		Sources:         []string{"adsb.lol", "opensky"},
		PositionQuality: flightmodel.PositionQualityADSB,
		LastSeenUTC:     time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC),
	}
}

func TestWriteAndReadFlightState(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()
	want := sampleFlightState("a1b2c3", 37.6188, -122.3758)

	if err := c.WriteFlightState(ctx, want, time.Minute); err != nil {
		t.Fatalf("WriteFlightState: %v", err)
	}

	got, err := c.ReadFlightState(ctx, "a1b2c3")
	if err != nil {
		t.Fatalf("ReadFlightState: %v", err)
	}

	if got.ICAO24 != want.ICAO24 || got.Lat != want.Lat || got.Lon != want.Lon {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if got.Callsign == nil || *got.Callsign != *want.Callsign {
		t.Errorf("Callsign = %v, want %v", got.Callsign, *want.Callsign)
	}
	if got.AltitudeBaroFt == nil || *got.AltitudeBaroFt != *want.AltitudeBaroFt {
		t.Errorf("AltitudeBaroFt = %v, want %v", got.AltitudeBaroFt, *want.AltitudeBaroFt)
	}
	if len(got.Sources) != 2 || got.Sources[0] != "adsb.lol" || got.Sources[1] != "opensky" {
		t.Errorf("Sources = %v, want [adsb.lol opensky]", got.Sources)
	}
	if got.PositionQuality != want.PositionQuality {
		t.Errorf("PositionQuality = %v, want %v", got.PositionQuality, want.PositionQuality)
	}
	if !got.LastSeenUTC.Equal(want.LastSeenUTC) {
		t.Errorf("LastSeenUTC = %v, want %v", got.LastSeenUTC, want.LastSeenUTC)
	}
}

func TestWriteAndReadFlightStateTypeFields(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	want := sampleFlightState("ae1234", 34.05, -118.24)
	acType := "KC135"
	cat := "A5"
	iconClass := "tanker"
	want.AircraftType = &acType
	want.EmitterCategory = &cat
	want.Military = true
	want.IconClass = &iconClass

	if err := c.WriteFlightState(ctx, want, time.Minute); err != nil {
		t.Fatalf("WriteFlightState: %v", err)
	}
	got, err := c.ReadFlightState(ctx, "ae1234")
	if err != nil {
		t.Fatalf("ReadFlightState: %v", err)
	}

	if got.AircraftType == nil || *got.AircraftType != acType {
		t.Errorf("AircraftType = %v, want %s", got.AircraftType, acType)
	}
	if got.EmitterCategory == nil || *got.EmitterCategory != cat {
		t.Errorf("EmitterCategory = %v, want %s", got.EmitterCategory, cat)
	}
	if !got.Military {
		t.Error("Military = false, want true")
	}
	if got.IconClass == nil || *got.IconClass != iconClass {
		t.Errorf("IconClass = %v, want %s", got.IconClass, iconClass)
	}
}

func TestReadFlightStateTypeFieldsDefaultWhenAbsent(t *testing.T) {
	// A flight written without the type fields (e.g. OpenSky-only) must
	// decode with nil type fields and Military false, not a decode error.
	c := newTestClient(t)
	ctx := context.Background()

	want := sampleFlightState("a1b2c3", 37.6188, -122.3758)
	if err := c.WriteFlightState(ctx, want, time.Minute); err != nil {
		t.Fatalf("WriteFlightState: %v", err)
	}
	got, err := c.ReadFlightState(ctx, "a1b2c3")
	if err != nil {
		t.Fatalf("ReadFlightState: %v", err)
	}
	if got.AircraftType != nil || got.EmitterCategory != nil || got.Military || got.IconClass != nil {
		t.Errorf("expected nil/false type fields, got type=%v cat=%v mil=%v icon=%v",
			got.AircraftType, got.EmitterCategory, got.Military, got.IconClass)
	}
}

func TestReadFlightStateMissingReturnsRedisNil(t *testing.T) {
	c := newTestClient(t)
	_, err := c.ReadFlightState(context.Background(), "doesnotexist")
	if !errors.Is(err, redis.Nil) {
		t.Fatalf("err = %v, want wrapped redis.Nil", err)
	}
}

func TestWriteFlightStateRejectsMissingICAO24(t *testing.T) {
	c := newTestClient(t)
	if err := c.WriteFlightState(context.Background(), flightmodel.FlightState{}, time.Minute); err == nil {
		t.Fatal("WriteFlightState(empty icao24): want error, got nil")
	}
}

func TestWriteFlightStateOverwritesStaleOptionalFields(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	callsign := "UAL123"
	first := flightmodel.FlightState{
		ICAO24:          "a1b2c3",
		Callsign:        &callsign,
		LastSeenUTC:     time.Now().UTC(),
		PositionQuality: flightmodel.PositionQualityADSB,
	}
	if err := c.WriteFlightState(ctx, first, time.Minute); err != nil {
		t.Fatalf("WriteFlightState(first): %v", err)
	}

	// Second winner no longer reports a callsign; the stale value from the
	// first write must not leak through.
	second := flightmodel.FlightState{
		ICAO24:          "a1b2c3",
		LastSeenUTC:     time.Now().UTC(),
		PositionQuality: flightmodel.PositionQualityADSB,
	}
	if err := c.WriteFlightState(ctx, second, time.Minute); err != nil {
		t.Fatalf("WriteFlightState(second): %v", err)
	}

	got, err := c.ReadFlightState(ctx, "a1b2c3")
	if err != nil {
		t.Fatalf("ReadFlightState: %v", err)
	}
	if got.Callsign != nil {
		t.Errorf("Callsign = %v, want nil (stale value should not persist)", *got.Callsign)
	}
}

func TestFlightKeyFormat(t *testing.T) {
	got := FlightKey("A1B2C3")
	want := "flight:a1b2c3"
	if got != want {
		t.Errorf("FlightKey = %q, want %q", got, want)
	}
}

func TestQueryFlightsByBBoxReturnsOnlyAircraftInside(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	inside := sampleFlightState("aaaaaa", 37.0, -122.0)
	alsoInside := sampleFlightState("bbbbbb", 37.5, -121.5)
	outside := sampleFlightState("cccccc", 50.0, -100.0)

	for _, s := range []flightmodel.FlightState{inside, alsoInside, outside} {
		if err := c.WriteFlightState(ctx, s, time.Minute); err != nil {
			t.Fatalf("WriteFlightState(%s): %v", s.ICAO24, err)
		}
	}

	bbox, err := geo.ParseBBox("-123,36,-121,38")
	if err != nil {
		t.Fatalf("ParseBBox: %v", err)
	}

	got, err := c.QueryFlightsByBBox(ctx, bbox)
	if err != nil {
		t.Fatalf("QueryFlightsByBBox: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("QueryFlightsByBBox returned %d states, want 2: %+v", len(got), got)
	}
	if got[0].ICAO24 != "aaaaaa" || got[1].ICAO24 != "bbbbbb" {
		t.Errorf("ICAO24s = [%s, %s], want [aaaaaa, bbbbbb]", got[0].ICAO24, got[1].ICAO24)
	}
}

// TestQueryFlightsByBBoxGlobalReturnsAllAircraft covers the full-scan
// branch taken when a viewport is larger than maxGeoRadiusKM. In
// production a near-global bbox returned zero aircraft because Redis
// GEORADIUS's geohash search fails for planetary-scale radii; for such
// viewports the query must enumerate the whole geo set instead, so even
// aircraft on opposite sides of the globe come back.
func TestQueryFlightsByBBoxGlobalReturnsAllAircraft(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	spread := []flightmodel.FlightState{
		sampleFlightState("aaaaaa", 37.0, -122.0), // North America
		sampleFlightState("bbbbbb", -33.9, 151.2), // Australia
		sampleFlightState("cccccc", 64.1, -21.9),  // Iceland
		sampleFlightState("dddddd", -54.8, -68.3), // South America
		sampleFlightState("eeeeee", 1.35, 103.8),  // Singapore
	}
	for _, s := range spread {
		if err := c.WriteFlightState(ctx, s, time.Minute); err != nil {
			t.Fatalf("WriteFlightState(%s): %v", s.ICAO24, err)
		}
	}

	bbox, err := geo.ParseBBox("-180,-90,180,90")
	if err != nil {
		t.Fatalf("ParseBBox: %v", err)
	}
	if bbox.RadiusKM() <= maxGeoRadiusKM {
		t.Fatalf("test bbox radius %.0fkm must exceed maxGeoRadiusKM %d to exercise the full-scan branch", bbox.RadiusKM(), maxGeoRadiusKM)
	}

	got, err := c.QueryFlightsByBBox(ctx, bbox)
	if err != nil {
		t.Fatalf("QueryFlightsByBBox: %v", err)
	}
	if len(got) != len(spread) {
		t.Fatalf("QueryFlightsByBBox(global) returned %d states, want %d: %+v", len(got), len(spread), got)
	}
}

func TestPruneExpiredGeoMembersRemovesEntriesWithoutAHash(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	live := sampleFlightState("aaaaaa", 37.0, -122.0)
	expired := sampleFlightState("bbbbbb", 38.0, -123.0)
	for _, s := range []flightmodel.FlightState{live, expired} {
		if err := c.WriteFlightState(ctx, s, time.Minute); err != nil {
			t.Fatalf("WriteFlightState(%s): %v", s.ICAO24, err)
		}
	}

	// Simulate "bbbbbb"'s hash TTLing out while its flights:geo entry (which
	// has no TTL of its own) lives on.
	if err := c.rdb.Del(ctx, FlightKey("bbbbbb")).Err(); err != nil {
		t.Fatalf("Del: %v", err)
	}

	pruned, err := c.PruneExpiredGeoMembers(ctx)
	if err != nil {
		t.Fatalf("PruneExpiredGeoMembers: %v", err)
	}
	if pruned != 1 {
		t.Fatalf("PruneExpiredGeoMembers = %d, want 1", pruned)
	}

	members, err := c.rdb.ZRange(ctx, GeoSetKey, 0, -1).Result()
	if err != nil {
		t.Fatalf("ZRange: %v", err)
	}
	if len(members) != 1 || members[0] != "aaaaaa" {
		t.Errorf("flights:geo members = %v, want [aaaaaa]", members)
	}
}

func TestPruneExpiredGeoMembersIsNoopWhenAllLive(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	if err := c.WriteFlightState(ctx, sampleFlightState("aaaaaa", 37.0, -122.0), time.Minute); err != nil {
		t.Fatalf("WriteFlightState: %v", err)
	}

	pruned, err := c.PruneExpiredGeoMembers(ctx)
	if err != nil {
		t.Fatalf("PruneExpiredGeoMembers: %v", err)
	}
	if pruned != 0 {
		t.Errorf("PruneExpiredGeoMembers = %d, want 0", pruned)
	}
}

func TestQueryFlightsByBBoxReturnsEmptyWhenNoneMatch(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	outside := sampleFlightState("cccccc", 50.0, -100.0)
	if err := c.WriteFlightState(ctx, outside, time.Minute); err != nil {
		t.Fatalf("WriteFlightState: %v", err)
	}

	bbox, err := geo.ParseBBox("-123,36,-121,38")
	if err != nil {
		t.Fatalf("ParseBBox: %v", err)
	}

	got, err := c.QueryFlightsByBBox(ctx, bbox)
	if err != nil {
		t.Fatalf("QueryFlightsByBBox: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("QueryFlightsByBBox returned %d states, want 0: %+v", len(got), got)
	}
}
