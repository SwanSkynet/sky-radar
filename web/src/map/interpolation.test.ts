import { describe, it, expect } from "vitest";
import type { FlightState } from "../api/flights";
import {
  applyInterpolation,
  deadReckon,
  interpolatedPosition,
  shouldInterpolate,
  MAX_EXTRAPOLATION_S,
  type Anchor,
} from "./interpolation";

function flight(overrides: Partial<FlightState> = {}): FlightState {
  return {
    icao24: "abc123",
    callsign: null,
    registration: null,
    lat: 34,
    lon: -118,
    altitude_baro_ft: 30000,
    altitude_geo_ft: null,
    ground_speed_kt: 480,
    vertical_rate_fpm: null,
    heading_deg: 90,
    on_ground: false,
    squawk: null,
    sources: [],
    position_quality: "adsb",
    last_seen_utc: "2026-01-01T00:00:00Z",
    stale: false,
    aircraft_type: null,
    emitter_category: null,
    military: false,
    icon_class: null,
    ...overrides,
  };
}

describe("shouldInterpolate", () => {
  it("interpolates a moving airborne fresh aircraft", () => {
    expect(shouldInterpolate(flight())).toBe(true);
  });
  it("skips on-ground, stale, and motionless aircraft", () => {
    expect(shouldInterpolate(flight({ on_ground: true }))).toBe(false);
    expect(shouldInterpolate(flight({ stale: true }))).toBe(false);
    expect(shouldInterpolate(flight({ ground_speed_kt: 0 }))).toBe(false);
    expect(shouldInterpolate(flight({ ground_speed_kt: null }))).toBe(false);
    expect(shouldInterpolate(flight({ heading_deg: null }))).toBe(false);
  });

  it("skips non-finite speed/heading so dead reckoning never yields NaN", () => {
    expect(shouldInterpolate(flight({ heading_deg: NaN }))).toBe(false);
    expect(shouldInterpolate(flight({ ground_speed_kt: NaN }))).toBe(false);
    expect(shouldInterpolate(flight({ ground_speed_kt: Infinity }))).toBe(
      false,
    );
  });
});

describe("deadReckon", () => {
  it("advances eastward (heading 90) by increasing longitude", () => {
    const [lon, lat] = deadReckon(34, -118, 480, 90, 60);
    expect(lon).toBeGreaterThan(-118);
    expect(lat).toBeCloseTo(34, 2);
  });

  it("advances northward (heading 0) by increasing latitude", () => {
    const [lon, lat] = deadReckon(34, -118, 480, 0, 60);
    expect(lat).toBeGreaterThan(34);
    expect(lon).toBeCloseTo(-118, 5);
  });

  it("caps extrapolation distance at MAX_EXTRAPOLATION_S", () => {
    const capped = deadReckon(34, -118, 480, 90, 10_000);
    const atMax = deadReckon(34, -118, 480, 90, MAX_EXTRAPOLATION_S);
    expect(capped).toEqual(atMax);
  });

  it("does not move with zero elapsed time", () => {
    const [lon, lat] = deadReckon(34, -118, 480, 90, 0);
    expect(lon).toBeCloseTo(-118, 9);
    expect(lat).toBeCloseTo(34, 9);
  });
});

describe("interpolatedPosition", () => {
  it("returns the anchor position for ineligible aircraft", () => {
    expect(
      interpolatedPosition(flight({ on_ground: true }), 34, -118, 60),
    ).toEqual([-118, 34]);
  });
});

describe("applyInterpolation", () => {
  it("advances a moving aircraft over time from a fixed anchor", () => {
    const anchors = new Map<string, Anchor>();
    const t0 = 1_000_000;
    const f = flight();

    const first = applyInterpolation([f], anchors, t0);
    expect(first[0].lon).toBeCloseTo(-118, 6); // anchor just set, elapsed ~0

    const later = applyInterpolation([f], anchors, t0 + 10_000);
    expect(later[0].lon).toBeGreaterThan(-118); // moved east after 10s
  });

  it("snaps to the authoritative position when a new update arrives", () => {
    const anchors = new Map<string, Anchor>();
    const t0 = 1_000_000;

    applyInterpolation([flight()], anchors, t0);
    // 20s later, same update: aircraft has drifted east.
    const drifted = applyInterpolation([flight()], anchors, t0 + 20_000);
    expect(drifted[0].lon).toBeGreaterThan(-118);

    // A fresh server update (new last_seen_utc, authoritative lon) resets the
    // anchor and snaps back to server truth.
    const updated = flight({
      last_seen_utc: "2026-01-01T00:00:20Z",
      lon: -117.5,
      lat: 34,
    });
    const snapped = applyInterpolation([updated], anchors, t0 + 20_000);
    expect(snapped[0].lon).toBeCloseTo(-117.5, 6);
  });

  it("prunes anchors for aircraft no longer present", () => {
    const anchors = new Map<string, Anchor>();
    applyInterpolation(
      [flight({ icao24: "a" }), flight({ icao24: "b" })],
      anchors,
      1,
    );
    expect(anchors.size).toBe(2);

    applyInterpolation([flight({ icao24: "a" })], anchors, 2);
    expect([...anchors.keys()]).toEqual(["a"]);
  });

  it("leaves stale/on-ground aircraft at their authoritative position", () => {
    const anchors = new Map<string, Anchor>();
    const t0 = 1_000_000;
    const grounded = flight({ on_ground: true });

    applyInterpolation([grounded], anchors, t0);
    const later = applyInterpolation([grounded], anchors, t0 + 30_000);
    expect(later[0].lon).toBe(-118);
    expect(later[0].lat).toBe(34);
  });
});
