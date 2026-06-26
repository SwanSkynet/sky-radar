// Typed client for GET /replay (docs/prd/phase-2-realtime-systems.md
// P2-FR5). The gateway reconstructs the requested window from whichever
// of JetStream's retained flights.updates / Postgres flight_history
// covers each part of it, but always returns this one flat sample shape.

import { API_V1_BASE_URL, bboxToQueryValue } from "./flights";
import type { BBox } from "./flights";

// ReplaySample mirrors internal/pgstore.FlightHistoryRecord's JSON shape,
// which in turn matches the flight_history schema in
// docs/architecture/data-model.md.
export interface ReplaySample {
  icao24: string;
  recorded_at: string;
  lat: number;
  lon: number;
  altitude_baro_ft: number | null;
  ground_speed_kt: number | null;
  heading_deg: number | null;
  on_ground: boolean;
}

// fetchReplayWindow queries GET /replay?from=&to=[&bbox=]. from/to are
// sent as RFC3339 (native Date.toISOString() output), matching the
// gateway's parsing in cmd/apigateway/replay.go.
export async function fetchReplayWindow(
  from: Date,
  to: Date,
  bbox?: BBox,
  signal?: AbortSignal,
): Promise<ReplaySample[]> {
  const params = new URLSearchParams({
    from: from.toISOString(),
    to: to.toISOString(),
  });
  if (bbox) params.set("bbox", bboxToQueryValue(bbox));

  const url = `${API_V1_BASE_URL}/replay?${params.toString()}`;
  const res = await fetch(url, { signal });
  if (!res.ok) {
    throw new Error(`GET /replay failed: ${res.status} ${res.statusText}`);
  }
  return (await res.json()) as ReplaySample[];
}
