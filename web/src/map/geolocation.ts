// Initial map center from the visitor's browser geolocation. This is a
// per-user *view* default only — it never changes what the backend ingests
// or how the viewport query works (see Feature 0 in
// docs/features/batch-1-coverage-detail-icons-cadence.md).

// World-view fallback, matching the map's initial camera. Used when
// geolocation is denied, errors, or is unsupported.
export const WORLD_VIEW = {
  center: [0, 20] as [number, number],
  zoom: 2,
};

// Zoom level eased to once the visitor's location is known — a regional view
// that shows nearby traffic without being so tight it's empty.
export const GEOLOCATED_ZOOM = 7;

// Minimal structural type for the bits of a maplibre Map this module drives,
// so the helper stays testable without a real GL map.
export interface CameraMap {
  easeTo(options: { center: [number, number]; zoom: number }): void;
}

// easeToUserLocation asks for the visitor's location and, if granted, eases
// the map to it. It does nothing (leaving the world view in place) when
// geolocation is denied, errors, times out, or is unavailable (e.g. an
// insecure context where navigator.geolocation is undefined). It never blocks
// first paint: getCurrentPosition is async, so the map renders at the world
// view immediately and only re-centers if/when permission is granted.
export function easeToUserLocation(
  map: CameraMap,
  geolocation: Geolocation | undefined = typeof navigator !== "undefined"
    ? navigator.geolocation
    : undefined,
): void {
  if (!geolocation || typeof geolocation.getCurrentPosition !== "function") {
    return;
  }
  geolocation.getCurrentPosition(
    (position) => {
      map.easeTo({
        center: [position.coords.longitude, position.coords.latitude],
        zoom: GEOLOCATED_ZOOM,
      });
    },
    () => {
      // Denied / unavailable / timed out: keep the world view.
    },
    { enableHighAccuracy: false, timeout: 8000, maximumAge: 600_000 },
  );
}
