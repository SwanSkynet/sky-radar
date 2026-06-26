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
}

export interface BBox {
  minLon: number;
  minLat: number;
  maxLon: number;
  maxLat: number;
}

export const API_BASE_URL: string =
  import.meta.env.VITE_API_BASE_URL ?? "http://localhost:8080";

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
  const url = `${API_BASE_URL}/flights?bbox=${encodeURIComponent(bboxToQueryValue(bbox))}`;
  const res = await fetch(url, { signal });
  if (!res.ok) {
    throw new Error(`GET /flights failed: ${res.status} ${res.statusText}`);
  }
  return (await res.json()) as FlightState[];
}
