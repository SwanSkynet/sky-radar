// Typed client for the minimal REST API (docs/prd/phase-1-foundation.md:
// GET /flights, GET /flights/{icao24}). Field shape must match the
// canonical FlightState in docs/architecture/data-model.md and
// internal/flightmodel/flightstate.go exactly.

export type PositionQuality = "adsb" | "mlat" | "estimated";

export interface FlightState {
  icao24: string;
  callsign: string | null;
  registration: string | null;
  lat: number;
  lon: number;
  altitude_baro_ft: number | null;
  altitude_geo_ft: number | null;
  ground_speed_kt: number | null;
  vertical_rate_fpm: number | null;
  heading_deg: number | null;
  on_ground: boolean;
  squawk: string | null;
  sources: string[];
  position_quality: PositionQuality;
  last_seen_utc: string;
  stale: boolean;
  // Aircraft type metadata (phase-5 batch 1, Feature 2). Captured from
  // adsb.lol / airplanes.live only; null/false for OpenSky-only aircraft
  // (type can still be present when OpenSky wins the positional merge but a
  // regional provider also reported the aircraft).
  aircraft_type: string | null;
  emitter_category: string | null;
  military: boolean;
  // Derived icon bucket (an SVG name in src/assets), or null when nothing
  // classifiable was available — the map then draws a default icon.
  icon_class: string | null;
}

export interface BBox {
  minLon: number;
  minLat: number;
  maxLon: number;
  maxLat: number;
}

export const API_BASE_URL: string =
  import.meta.env.VITE_API_BASE_URL ?? "http://localhost:8080";

// API_V1_BASE_URL is the versioned public API root per
// docs/prd/phase-2-realtime-systems.md's "Public API v1... versioned
// (/api/v1)" requirement — every REST/WebSocket client in this directory
// builds its URL from this, not API_BASE_URL directly. API_BASE_URL's
// trailing slash (if VITE_API_BASE_URL was configured with one) is
// stripped first so this never doubles up to ".../api/v1".
export const API_V1_BASE_URL = `${API_BASE_URL.replace(/\/+$/, "")}/api/v1`;

export function bboxToQueryValue(bbox: BBox): string {
  return `${bbox.minLon},${bbox.minLat},${bbox.maxLon},${bbox.maxLat}`;
}

// fetchFlightsByBBox queries GET /flights?bbox=... per P1-FR4. Callers
// own clamping/validating bbox — the API rejects out-of-range or
// inverted boxes with a 400.
export async function fetchFlightsByBBox(
  bbox: BBox,
  signal?: AbortSignal,
): Promise<FlightState[]> {
  const url = `${API_V1_BASE_URL}/flights?bbox=${encodeURIComponent(bboxToQueryValue(bbox))}`;
  const res = await fetch(url, { signal });
  if (!res.ok) {
    throw new Error(`GET /flights failed: ${res.status} ${res.statusText}`);
  }
  return (await res.json()) as FlightState[];
}
