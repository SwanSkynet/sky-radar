package flightmodel

import (
	"crypto/rand"
	"fmt"
)

// NewID returns a randomly generated UUID (v4, RFC 4122), used as the
// primary key for any canonical entity this package defines (Event, Zone,
// WatchlistEntry) that needs a server-generated ID.
func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("flightmodel: read random bytes for id: %v", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
