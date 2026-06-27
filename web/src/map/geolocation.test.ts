import { describe, it, expect, vi } from "vitest";
import {
  easeToUserLocation,
  GEOLOCATED_ZOOM,
  type CameraMap,
} from "./geolocation";

function fakeMap() {
  const easeTo = vi.fn();
  const map: CameraMap = { easeTo };
  return { map, easeTo };
}

describe("easeToUserLocation", () => {
  it("eases to the visitor's location when permission is granted", () => {
    const { map, easeTo } = fakeMap();
    const geolocation = {
      getCurrentPosition: (success: PositionCallback) =>
        success({
          coords: { longitude: -118.24, latitude: 34.05 },
        } as GeolocationPosition),
    } as unknown as Geolocation;

    easeToUserLocation(map, geolocation);

    expect(easeTo).toHaveBeenCalledWith({
      center: [-118.24, 34.05],
      zoom: GEOLOCATED_ZOOM,
    });
  });

  it("keeps the world view when permission is denied or it errors", () => {
    const { map, easeTo } = fakeMap();
    const geolocation = {
      getCurrentPosition: (
        _success: PositionCallback,
        error: PositionErrorCallback,
      ) => error({ code: 1, message: "denied" } as GeolocationPositionError),
    } as unknown as Geolocation;

    easeToUserLocation(map, geolocation);

    expect(easeTo).not.toHaveBeenCalled();
  });

  it("does nothing when geolocation is unavailable (insecure context)", () => {
    const { map, easeTo } = fakeMap();

    expect(() => easeToUserLocation(map, undefined)).not.toThrow();
    expect(easeTo).not.toHaveBeenCalled();
  });
});
