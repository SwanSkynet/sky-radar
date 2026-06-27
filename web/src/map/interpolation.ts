// Client-side dead-reckoning interpolation so aircraft glide between server
// updates instead of teleporting every cadence step (phase-5 batch 1,
// Feature 3). Positions are extrapolated from each aircraft's last known
// lat/lon, ground_speed_kt and heading_deg, and snap back to the
// authoritative position when the next real update arrives.

import type { FlightState } from "../api/flights";

// Cap on how far past its last update an aircraft is extrapolated, so a lost
// aircraft (no further updates) doesn't drift off the map indefinitely.
export const MAX_EXTRAPOLATION_S = 30;

const EARTH_RADIUS_M = 6_371_000;
const NM_TO_M = 1852;

// Anchor records the authoritative position an aircraft was last reported at,
// plus the client clock time we received it, so elapsed time is measured
// against the client clock (immune to client/server clock skew).
export interface Anchor {
  lat: number;
  lon: number;
  lastSeenUtc: string;
  clientMs: number;
}

// shouldInterpolate reports whether an aircraft is eligible for dead
// reckoning. On-ground and stale aircraft are left at their authoritative
// position, as are aircraft missing the speed/heading needed to extrapolate.
export function shouldInterpolate(flight: FlightState): boolean {
  return (
    !flight.on_ground &&
    !flight.stale &&
    flight.ground_speed_kt != null &&
    flight.ground_speed_kt > 0 &&
    flight.heading_deg != null
  );
}

// deadReckon advances [lon, lat] from a start point along a heading (degrees
// clockwise from north) at a ground speed (knots) for elapsedSeconds, clamped
// to [0, MAX_EXTRAPOLATION_S]. Uses the spherical forward-geodesic formula.
export function deadReckon(
  lat: number,
  lon: number,
  speedKt: number,
  headingDeg: number,
  elapsedSeconds: number,
): [number, number] {
  const capped = Math.min(Math.max(elapsedSeconds, 0), MAX_EXTRAPOLATION_S);
  const distanceM = speedKt * (capped / 3600) * NM_TO_M;
  const angular = distanceM / EARTH_RADIUS_M;
  const bearing = (headingDeg * Math.PI) / 180;
  const lat1 = (lat * Math.PI) / 180;
  const lon1 = (lon * Math.PI) / 180;

  const lat2 = Math.asin(
    Math.sin(lat1) * Math.cos(angular) +
      Math.cos(lat1) * Math.sin(angular) * Math.cos(bearing),
  );
  const lon2 =
    lon1 +
    Math.atan2(
      Math.sin(bearing) * Math.sin(angular) * Math.cos(lat1),
      Math.cos(angular) - Math.sin(lat1) * Math.sin(lat2),
    );

  return [(lon2 * 180) / Math.PI, (lat2 * 180) / Math.PI];
}

// interpolatedPosition returns the position to render an aircraft at, given
// its anchor and how long ago (client clock) the anchor was set. Ineligible
// aircraft render at their anchor (authoritative) position.
export function interpolatedPosition(
  flight: FlightState,
  anchorLat: number,
  anchorLon: number,
  elapsedSeconds: number,
): [number, number] {
  if (!shouldInterpolate(flight)) return [anchorLon, anchorLat];
  return deadReckon(
    anchorLat,
    anchorLon,
    flight.ground_speed_kt as number,
    flight.heading_deg as number,
    elapsedSeconds,
  );
}

// applyInterpolation returns a copy of flights with lat/lon advanced by dead
// reckoning, updating the anchors map in place: a new last_seen_utc resets an
// aircraft's anchor to the authoritative position (snapping to server truth),
// and anchors for aircraft no longer present are pruned.
export function applyInterpolation(
  flights: FlightState[],
  anchors: Map<string, Anchor>,
  now: number,
): FlightState[] {
  const live = new Set<string>();
  const result = flights.map((flight) => {
    live.add(flight.icao24);
    let anchor = anchors.get(flight.icao24);
    if (!anchor || anchor.lastSeenUtc !== flight.last_seen_utc) {
      anchor = {
        lat: flight.lat,
        lon: flight.lon,
        lastSeenUtc: flight.last_seen_utc,
        clientMs: now,
      };
      anchors.set(flight.icao24, anchor);
    }
    const elapsed = (now - anchor.clientMs) / 1000;
    const [lon, lat] = interpolatedPosition(
      flight,
      anchor.lat,
      anchor.lon,
      elapsed,
    );
    return lon === flight.lon && lat === flight.lat
      ? flight
      : { ...flight, lat, lon };
  });

  for (const icao24 of anchors.keys()) {
    if (!live.has(icao24)) anchors.delete(icao24);
  }
  return result;
}
