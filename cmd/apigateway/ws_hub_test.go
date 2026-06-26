package main

import (
	"testing"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
	"github.com/SwanSkynet/sky-radar/internal/geo"
	"github.com/SwanSkynet/sky-radar/internal/natsutil"
)

func bbox(minLon, minLat, maxLon, maxLat float64) geo.BBox {
	return geo.BBox{MinLon: minLon, MinLat: minLat, MaxLon: maxLon, MaxLat: maxLat}
}

func flightAt(icao24 string, lat, lon float64) natsutil.FlightStateMessage {
	return natsutil.FlightStateMessage{
		Sequence: 1,
		State: flightmodel.FlightState{
			ICAO24:      icao24,
			Lat:         lat,
			Lon:         lon,
			LastSeenUTC: time.Now().UTC(),
		},
	}
}

func TestHubBroadcastOnlyDeliversToClientsWhoseBBoxContainsTheAircraft(t *testing.T) {
	hub := newWSHub()

	inSF := newWSClient()
	inSF.SetBBox(bbox(-123, 36, -121, 38)) // around San Francisco
	inNY := newWSClient()
	inNY.SetBBox(bbox(-75, 40, -73, 41)) // around New York
	hub.register(inSF)
	hub.register(inNY)

	hub.broadcast(flightAt("aaaaaa", 37.0, -122.0)) // inside SF bbox only

	select {
	case msg := <-inSF.send:
		if msg.State.ICAO24 != "aaaaaa" {
			t.Errorf("inSF received ICAO24 = %q, want aaaaaa", msg.State.ICAO24)
		}
	default:
		t.Fatal("inSF client did not receive the in-viewport update")
	}

	select {
	case msg := <-inNY.send:
		t.Fatalf("inNY client unexpectedly received an out-of-viewport update: %+v", msg)
	default:
	}
}

func TestHubUnregisterStopsFurtherDelivery(t *testing.T) {
	hub := newWSHub()
	c := newWSClient()
	c.SetBBox(bbox(-123, 36, -121, 38))
	hub.register(c)
	hub.unregister(c)

	hub.broadcast(flightAt("aaaaaa", 37.0, -122.0))

	select {
	case msg := <-c.send:
		t.Fatalf("unregistered client unexpectedly received a message: %+v", msg)
	default:
	}
}

func TestHubSetBBoxChangesWhichUpdatesAClientReceives(t *testing.T) {
	hub := newWSHub()
	c := newWSClient()
	c.SetBBox(bbox(-123, 36, -121, 38)) // SF
	hub.register(c)

	hub.broadcast(flightAt("aaaaaa", 40.7, -74.0)) // NY: out of viewport
	select {
	case msg := <-c.send:
		t.Fatalf("received update outside the original viewport: %+v", msg)
	default:
	}

	c.SetBBox(bbox(-75, 40, -73, 41)) // pan to NY
	hub.broadcast(flightAt("bbbbbb", 40.7, -74.0))
	select {
	case msg := <-c.send:
		if msg.State.ICAO24 != "bbbbbb" {
			t.Errorf("ICAO24 = %q, want bbbbbb", msg.State.ICAO24)
		}
	default:
		t.Fatal("did not receive update inside the updated viewport")
	}
}

func TestHubBroadcastDropsRatherThanBlocksOnAFullClientBuffer(t *testing.T) {
	hub := newWSHub()
	c := newWSClient()
	c.SetBBox(bbox(-123, 36, -121, 38))
	hub.register(c)

	for i := 0; i < wsSendBufferSize+10; i++ {
		hub.broadcast(flightAt("aaaaaa", 37.0, -122.0))
	}

	if len(c.send) != wsSendBufferSize {
		t.Errorf("len(c.send) = %d, want %d (buffer should be full, not blocked)", len(c.send), wsSendBufferSize)
	}
}

func TestHubClientCount(t *testing.T) {
	hub := newWSHub()
	if got := hub.clientCount(); got != 0 {
		t.Fatalf("clientCount = %d, want 0", got)
	}

	c1, c2 := newWSClient(), newWSClient()
	hub.register(c1)
	hub.register(c2)
	if got := hub.clientCount(); got != 2 {
		t.Fatalf("clientCount = %d, want 2", got)
	}

	hub.unregister(c1)
	if got := hub.clientCount(); got != 1 {
		t.Fatalf("clientCount = %d, want 1", got)
	}
}
