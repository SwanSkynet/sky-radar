import { describe, it, expect } from "vitest";
import { computeFlightsAtTime } from "./replayPlayback";
import type { ReplaySample } from "../api/replay";

function sample(overrides: Partial<ReplaySample> = {}): ReplaySample {
  return {
    icao24: "abc123",
    recorded_at: new Date(0).toISOString(),
    lat: 1,
    lon: 2,
    altitude_baro_ft: null,
    ground_speed_kt: null,
    heading_deg: null,
    on_ground: false,
    ...overrides,
  };
}

describe("computeFlightsAtTime", () => {
  it("returns an empty list when atMs is null", () => {
    expect(computeFlightsAtTime([sample()], null)).toEqual([]);
  });

  it("reconstructs each aircraft's most recent sample at or before atMs", () => {
    // Samples must be globally time-ascending across all aircraft, matching
    // GET /replay's ordering guarantee (computeFlightsAtTime relies on this
    // to stop scanning early once it passes atMs).
    const samples: ReplaySample[] = [
      sample({ icao24: "a", recorded_at: new Date(1000).toISOString(), lat: 1 }),
      sample({ icao24: "b", recorded_at: new Date(1500).toISOString(), lat: 9 }),
      sample({ icao24: "a", recorded_at: new Date(2000).toISOString(), lat: 2 }),
    ];

    const flights = computeFlightsAtTime(samples, 1800);

    const byIcao = Object.fromEntries(flights.map((f) => [f.icao24, f]));
    expect(byIcao.a.lat).toBe(1); // a's sample at 2000 is after atMs, so its 1000 sample wins
    expect(byIcao.b.lat).toBe(9);
  });

  it("excludes aircraft whose only samples are after atMs", () => {
    const samples: ReplaySample[] = [
      sample({ icao24: "future", recorded_at: new Date(5000).toISOString() }),
    ];

    expect(computeFlightsAtTime(samples, 1000)).toEqual([]);
  });

  it("includes a sample exactly at atMs", () => {
    const samples: ReplaySample[] = [
      sample({ icao24: "a", recorded_at: new Date(2000).toISOString() }),
    ];

    expect(computeFlightsAtTime(samples, 2000)).toHaveLength(1);
  });

  it("fills in canonical FlightState fields the sample doesn't carry", () => {
    const samples: ReplaySample[] = [
      sample({
        icao24: "a",
        recorded_at: new Date(1000).toISOString(),
        altitude_baro_ft: 35000,
        ground_speed_kt: 420,
        heading_deg: 270,
        on_ground: true,
      }),
    ];

    const [flight] = computeFlightsAtTime(samples, 1000);
    expect(flight.altitude_baro_ft).toBe(35000);
    expect(flight.ground_speed_kt).toBe(420);
    expect(flight.heading_deg).toBe(270);
    expect(flight.on_ground).toBe(true);
    expect(flight.callsign).toBeNull();
    expect(flight.stale).toBe(false);
  });
});
