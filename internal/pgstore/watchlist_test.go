package pgstore

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
)

func testWatchlistEntry(session string) flightmodel.WatchlistEntry {
	return flightmodel.WatchlistEntry{
		ID:               flightmodel.NewID(),
		ICAO24:           fmt.Sprintf("t%d", time.Now().UnixNano()),
		Label:            "Friend's flight",
		CreatedBySession: session,
		CreatedAt:        time.Now().UTC().Truncate(time.Microsecond),
	}
}

func TestInsertWatchlistEntryAndListBySession(t *testing.T) {
	store := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	session := fmt.Sprintf("session-%d", time.Now().UnixNano())
	entry := testWatchlistEntry(session)

	if err := store.InsertWatchlistEntry(ctx, entry); err != nil {
		t.Fatalf("InsertWatchlistEntry: %v", err)
	}

	got, err := store.ListWatchlistEntriesBySession(ctx, session)
	if err != nil {
		t.Fatalf("ListWatchlistEntriesBySession: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1: %+v", len(got), got)
	}
	if got[0].ID != entry.ID || got[0].ICAO24 != entry.ICAO24 || got[0].Label != entry.Label {
		t.Errorf("got %+v, want %+v", got[0], entry)
	}
}

func TestListWatchlistEntriesBySessionOnlyReturnsOwnSession(t *testing.T) {
	store := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sessionA := fmt.Sprintf("session-a-%d", time.Now().UnixNano())
	sessionB := fmt.Sprintf("session-b-%d", time.Now().UnixNano())

	if err := store.InsertWatchlistEntry(ctx, testWatchlistEntry(sessionA)); err != nil {
		t.Fatalf("InsertWatchlistEntry(A): %v", err)
	}
	if err := store.InsertWatchlistEntry(ctx, testWatchlistEntry(sessionB)); err != nil {
		t.Fatalf("InsertWatchlistEntry(B): %v", err)
	}

	got, err := store.ListWatchlistEntriesBySession(ctx, sessionA)
	if err != nil {
		t.Fatalf("ListWatchlistEntriesBySession: %v", err)
	}
	if len(got) != 1 || got[0].CreatedBySession != sessionA {
		t.Errorf("got %+v, want exactly one entry owned by %s", got, sessionA)
	}
}

func TestListAllWatchlistEntriesReturnsEveryOwner(t *testing.T) {
	store := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sessionA := fmt.Sprintf("session-a-%d", time.Now().UnixNano())
	sessionB := fmt.Sprintf("session-b-%d", time.Now().UnixNano())
	entryA := testWatchlistEntry(sessionA)
	entryB := testWatchlistEntry(sessionB)

	if err := store.InsertWatchlistEntry(ctx, entryA); err != nil {
		t.Fatalf("InsertWatchlistEntry(A): %v", err)
	}
	if err := store.InsertWatchlistEntry(ctx, entryB); err != nil {
		t.Fatalf("InsertWatchlistEntry(B): %v", err)
	}

	all, err := store.ListAllWatchlistEntries(ctx)
	if err != nil {
		t.Fatalf("ListAllWatchlistEntries: %v", err)
	}

	found := map[string]bool{}
	for _, e := range all {
		found[e.ID] = true
	}
	if !found[entryA.ID] || !found[entryB.ID] {
		t.Errorf("ListAllWatchlistEntries missing one of the two seeded entries: %+v", all)
	}
}

func TestDeleteWatchlistEntryRequiresMatchingSession(t *testing.T) {
	store := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	session := fmt.Sprintf("session-%d", time.Now().UnixNano())
	entry := testWatchlistEntry(session)
	if err := store.InsertWatchlistEntry(ctx, entry); err != nil {
		t.Fatalf("InsertWatchlistEntry: %v", err)
	}

	deleted, err := store.DeleteWatchlistEntry(ctx, entry.ID, "wrong-session")
	if err != nil {
		t.Fatalf("DeleteWatchlistEntry(wrong session): %v", err)
	}
	if deleted {
		t.Error("DeleteWatchlistEntry deleted an entry owned by a different session")
	}

	deleted, err = store.DeleteWatchlistEntry(ctx, entry.ID, session)
	if err != nil {
		t.Fatalf("DeleteWatchlistEntry: %v", err)
	}
	if !deleted {
		t.Error("DeleteWatchlistEntry reported no row deleted for the owning session")
	}

	got, err := store.ListWatchlistEntriesBySession(ctx, session)
	if err != nil {
		t.Fatalf("ListWatchlistEntriesBySession: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d entries after delete, want 0", len(got))
	}
}

func TestDeleteWatchlistEntryNonexistentReturnsFalse(t *testing.T) {
	store := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	deleted, err := store.DeleteWatchlistEntry(ctx, flightmodel.NewID(), "any-session")
	if err != nil {
		t.Fatalf("DeleteWatchlistEntry: %v", err)
	}
	if deleted {
		t.Error("DeleteWatchlistEntry reported a row deleted for a nonexistent id")
	}
}
