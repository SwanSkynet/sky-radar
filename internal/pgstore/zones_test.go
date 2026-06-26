package pgstore

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
)

func testZone(session string) flightmodel.Zone {
	return flightmodel.Zone{
		ID:   flightmodel.NewID(),
		Name: "Test Zone",
		Polygon: flightmodel.GeoJSONPolygon{
			Type: "Polygon",
			Coordinates: [][][]float64{{
				{-122.5, 37.5}, {-122.0, 37.5}, {-122.0, 38.0}, {-122.5, 38.0}, {-122.5, 37.5},
			}},
		},
		CreatedBySession: session,
		CreatedAt:        time.Now().UTC().Truncate(time.Microsecond),
	}
}

func TestInsertZoneAndListBySession(t *testing.T) {
	store := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	session := fmt.Sprintf("session-%d", time.Now().UnixNano())
	zone := testZone(session)

	if err := store.InsertZone(ctx, zone); err != nil {
		t.Fatalf("InsertZone: %v", err)
	}

	got, err := store.ListZonesBySession(ctx, session)
	if err != nil {
		t.Fatalf("ListZonesBySession: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d zones, want 1: %+v", len(got), got)
	}
	if got[0].ID != zone.ID || got[0].Name != zone.Name {
		t.Errorf("got %+v, want %+v", got[0], zone)
	}
	if len(got[0].Polygon.Coordinates) != 1 || len(got[0].Polygon.Coordinates[0]) != 5 {
		t.Errorf("polygon round trip mismatch: %+v", got[0].Polygon)
	}
}

func TestListZonesBySessionOnlyReturnsOwnSession(t *testing.T) {
	store := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sessionA := fmt.Sprintf("session-a-%d", time.Now().UnixNano())
	sessionB := fmt.Sprintf("session-b-%d", time.Now().UnixNano())

	if err := store.InsertZone(ctx, testZone(sessionA)); err != nil {
		t.Fatalf("InsertZone(A): %v", err)
	}
	if err := store.InsertZone(ctx, testZone(sessionB)); err != nil {
		t.Fatalf("InsertZone(B): %v", err)
	}

	got, err := store.ListZonesBySession(ctx, sessionA)
	if err != nil {
		t.Fatalf("ListZonesBySession: %v", err)
	}
	if len(got) != 1 || got[0].CreatedBySession != sessionA {
		t.Errorf("got %+v, want exactly one zone owned by %s", got, sessionA)
	}
}

func TestListAllZonesReturnsEveryOwner(t *testing.T) {
	store := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sessionA := fmt.Sprintf("session-a-%d", time.Now().UnixNano())
	sessionB := fmt.Sprintf("session-b-%d", time.Now().UnixNano())
	zoneA := testZone(sessionA)
	zoneB := testZone(sessionB)

	if err := store.InsertZone(ctx, zoneA); err != nil {
		t.Fatalf("InsertZone(A): %v", err)
	}
	if err := store.InsertZone(ctx, zoneB); err != nil {
		t.Fatalf("InsertZone(B): %v", err)
	}

	all, err := store.ListAllZones(ctx)
	if err != nil {
		t.Fatalf("ListAllZones: %v", err)
	}

	found := map[string]bool{}
	for _, z := range all {
		found[z.ID] = true
	}
	if !found[zoneA.ID] || !found[zoneB.ID] {
		t.Errorf("ListAllZones missing one of the two seeded zones: %+v", all)
	}
}

func TestDeleteZoneRequiresMatchingSession(t *testing.T) {
	store := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	session := fmt.Sprintf("session-%d", time.Now().UnixNano())
	zone := testZone(session)
	if err := store.InsertZone(ctx, zone); err != nil {
		t.Fatalf("InsertZone: %v", err)
	}

	deleted, err := store.DeleteZone(ctx, zone.ID, "wrong-session")
	if err != nil {
		t.Fatalf("DeleteZone(wrong session): %v", err)
	}
	if deleted {
		t.Error("DeleteZone deleted a zone owned by a different session")
	}

	deleted, err = store.DeleteZone(ctx, zone.ID, session)
	if err != nil {
		t.Fatalf("DeleteZone: %v", err)
	}
	if !deleted {
		t.Error("DeleteZone reported no row deleted for the owning session")
	}

	got, err := store.ListZonesBySession(ctx, session)
	if err != nil {
		t.Fatalf("ListZonesBySession: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d zones after delete, want 0", len(got))
	}
}

func TestDeleteZoneNonexistentReturnsFalse(t *testing.T) {
	store := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	deleted, err := store.DeleteZone(ctx, flightmodel.NewID(), "any-session")
	if err != nil {
		t.Fatalf("DeleteZone: %v", err)
	}
	if deleted {
		t.Error("DeleteZone reported a row deleted for a nonexistent id")
	}
}
