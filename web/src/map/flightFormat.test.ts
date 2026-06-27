import { describe, it, expect } from "vitest";
import type { FlightState } from "../api/flights";
import {
  aircraftTypeLabel,
  decodeSquawkEmergency,
  headerLabel,
  secondsAgo,
  verticalRateArrow,
} from "./flightFormat";

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

describe("decodeSquawkEmergency", () => {
  it.each([
    ["7500", "hijack"],
    ["7600", "radio failure"],
    ["7700", "general emergency"],
  ])("decodes %s as an emergency", (code, label) => {
    expect(decodeSquawkEmergency(code)).toBe(label);
  });

  it("returns null for normal and missing squawks", () => {
    expect(decodeSquawkEmergency("1200")).toBeNull();
    expect(decodeSquawkEmergency(null)).toBeNull();
    expect(decodeSquawkEmergency("")).toBeNull();
  });
});

describe("headerLabel", () => {
  it("uses the callsign when present", () => {
    expect(headerLabel(flight({ callsign: "UAL123 " }))).toBe("UAL123");
  });
  it("falls back to the uppercased hex when callsign is empty/null", () => {
    expect(headerLabel(flight({ callsign: null }))).toBe("ABC123");
    expect(headerLabel(flight({ callsign: "   " }))).toBe("ABC123");
  });
});

describe("verticalRateArrow", () => {
  it("indicates climb, descent, level, and unknown", () => {
    expect(verticalRateArrow(500)).toBe("▲");
    expect(verticalRateArrow(-500)).toBe("▼");
    expect(verticalRateArrow(0)).toBe("");
    expect(verticalRateArrow(null)).toBe("");
  });
});

describe("aircraftTypeLabel", () => {
  it("combines designator and bucket", () => {
    expect(
      aircraftTypeLabel(
        flight({ aircraft_type: "A320", icon_class: "commercial_jet" }),
      ),
    ).toBe("A320 · commercial jet");
  });
  it("returns null when no type is known", () => {
    expect(aircraftTypeLabel(flight())).toBeNull();
  });
});

describe("secondsAgo", () => {
  it("computes whole seconds since last seen", () => {
    const now = Date.parse("2026-01-01T00:00:30Z");
    expect(secondsAgo("2026-01-01T00:00:00Z", now)).toBe(30);
  });
  it("clamps to zero for future timestamps", () => {
    const now = Date.parse("2026-01-01T00:00:00Z");
    expect(secondsAgo("2026-01-01T00:01:00Z", now)).toBe(0);
  });
});
