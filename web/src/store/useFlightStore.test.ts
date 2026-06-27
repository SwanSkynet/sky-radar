import { describe, it, expect, beforeEach, vi, afterEach } from "vitest";
import { useFlightStore, STALE_THRESHOLD_MS } from "./useFlightStore";
import type { FlightState } from "../api/flights";

function flight(overrides: Partial<FlightState> = {}): FlightState {
  return {
    icao24: "abc123",
    callsign: null,
    registration: null,
    lat: 1,
    lon: 2,
    altitude_baro_ft: null,
    altitude_geo_ft: null,
    ground_speed_kt: null,
    vertical_rate_fpm: null,
    heading_deg: null,
    on_ground: false,
    squawk: null,
    sources: [],
    position_quality: "adsb",
    last_seen_utc: new Date().toISOString(),
    stale: false,
    aircraft_type: null,
    emitter_category: null,
    military: false,
    icon_class: null,
    ...overrides,
  };
}

describe("useFlightStore", () => {
  beforeEach(() => {
    useFlightStore.setState({
      flights: {},
      connectionStatus: "connecting",
      lastUpdated: null,
      error: null,
      selectedIcao24: null,
    });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("upserts a flight by icao24 without disturbing other tracked flights", () => {
    useFlightStore.getState().upsertFlight(flight({ icao24: "a" }));
    useFlightStore.getState().upsertFlight(flight({ icao24: "b" }));
    useFlightStore.getState().upsertFlight(flight({ icao24: "a", lat: 99 }));

    const { flights } = useFlightStore.getState();
    expect(Object.keys(flights).sort()).toEqual(["a", "b"]);
    expect(flights.a.lat).toBe(99);
  });

  it("retainWithinBBox drops aircraft outside the given viewport", () => {
    useFlightStore
      .getState()
      .upsertFlight(flight({ icao24: "in", lat: 1, lon: 1 }));
    useFlightStore
      .getState()
      .upsertFlight(flight({ icao24: "out", lat: 50, lon: 50 }));

    useFlightStore
      .getState()
      .retainWithinBBox({ minLon: 0, minLat: 0, maxLon: 10, maxLat: 10 });

    expect(Object.keys(useFlightStore.getState().flights)).toEqual(["in"]);
  });

  it("recomputeStaleness flags an aircraft stale once its last_seen_utc ages past the threshold, without a new push", () => {
    vi.useFakeTimers();
    const now = new Date("2026-01-01T00:00:00Z");
    vi.setSystemTime(now);

    useFlightStore.getState().upsertFlight(
      flight({
        icao24: "aging",
        stale: false,
        last_seen_utc: now.toISOString(),
      }),
    );

    vi.setSystemTime(new Date(now.getTime() + STALE_THRESHOLD_MS + 1000));
    useFlightStore.getState().recomputeStaleness();

    expect(useFlightStore.getState().flights.aging.stale).toBe(true);
  });

  it("select sets the selected icao24 and clearSelection resets it", () => {
    useFlightStore.getState().select("abc123");
    expect(useFlightStore.getState().selectedIcao24).toBe("abc123");

    useFlightStore.getState().clearSelection();
    expect(useFlightStore.getState().selectedIcao24).toBeNull();
  });

  it("retainWithinBBox clears the selection when the selected aircraft pans out of view", () => {
    useFlightStore
      .getState()
      .upsertFlight(flight({ icao24: "sel", lat: 1, lon: 1 }));
    useFlightStore.getState().select("sel");

    useFlightStore
      .getState()
      .retainWithinBBox({ minLon: 20, minLat: 20, maxLon: 30, maxLat: 30 });

    expect(useFlightStore.getState().selectedIcao24).toBeNull();
  });

  it("retainWithinBBox keeps the selection when the selected aircraft stays in view", () => {
    useFlightStore
      .getState()
      .upsertFlight(flight({ icao24: "sel", lat: 5, lon: 5 }));
    useFlightStore.getState().select("sel");

    useFlightStore
      .getState()
      .retainWithinBBox({ minLon: 0, minLat: 0, maxLon: 10, maxLat: 10 });

    expect(useFlightStore.getState().selectedIcao24).toBe("sel");
  });

  it("setConnectionStatus tracks realtime-path health independently of per-aircraft staleness", () => {
    useFlightStore
      .getState()
      .upsertFlight(flight({ icao24: "fresh", stale: false }));

    useFlightStore.getState().setConnectionStatus("reconnecting");

    expect(useFlightStore.getState().connectionStatus).toBe("reconnecting");
    expect(useFlightStore.getState().flights.fresh.stale).toBe(false);
  });
});
