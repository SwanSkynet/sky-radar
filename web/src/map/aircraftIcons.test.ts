import { describe, it, expect } from "vitest";
import {
  DEFAULT_ICON_CLASS,
  ICON_URLS,
  headingToIconAngle,
  iconDefFor,
  resolveIconClass,
} from "./aircraftIcons";

describe("resolveIconClass", () => {
  it("returns a known class unchanged", () => {
    expect(resolveIconClass("awacs")).toBe("awacs");
    expect(resolveIconClass("military_drone")).toBe("military_drone");
  });

  it("falls back to the default for null/unknown classes", () => {
    expect(resolveIconClass(null)).toBe(DEFAULT_ICON_CLASS);
    expect(resolveIconClass(undefined)).toBe(DEFAULT_ICON_CLASS);
    expect(resolveIconClass("nonexistent_bucket")).toBe(DEFAULT_ICON_CLASS);
  });

  it("has a URL for every classifier bucket plus the default", () => {
    for (const bucket of [
      "commercial_jet",
      "widebody",
      "business_jet",
      "cargo",
      "turboprop",
      "utility_helicopter",
      "attack_helicopter",
      "military_transport",
      "military_drone",
      "tanker",
      "awacs",
      "reconnaissance",
      "maritime_patrol",
      "stealth",
    ]) {
      expect(ICON_URLS[bucket]).toBeTruthy();
    }
    expect(ICON_URLS[DEFAULT_ICON_CLASS]).toBeTruthy();
  });
});

describe("iconDefFor", () => {
  it("builds a mask icon def keyed by the resolved class", () => {
    const def = iconDefFor("tanker");
    expect(def.id).toBe("tanker");
    expect(def.url).toBe(ICON_URLS.tanker);
    expect(def.mask).toBe(true);
    expect(def.anchorX).toBe(def.width / 2);
    expect(def.anchorY).toBe(def.height / 2);
  });

  it("uses the default icon for unknown/OpenSky aircraft", () => {
    expect(iconDefFor(null).id).toBe(DEFAULT_ICON_CLASS);
  });
});

describe("headingToIconAngle", () => {
  it("converts clockwise-from-north heading to counter-clockwise angle", () => {
    expect(headingToIconAngle(0)).toBe(0);
    expect(headingToIconAngle(90)).toBe(-90);
    expect(headingToIconAngle(270)).toBe(-270);
  });

  it("points north for a null/NaN heading", () => {
    expect(headingToIconAngle(null)).toBe(0);
    expect(headingToIconAngle(undefined)).toBe(0);
    expect(headingToIconAngle(NaN)).toBe(0);
  });
});
