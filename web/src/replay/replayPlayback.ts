import type { FlightState } from "../api/flights";
import type { ReplaySample } from "../api/replay";

// computeFlightsAtTime reconstructs each aircraft's most recent known
// position at or before atMs from a time-ascending list of replay
// samples. This is the same "apply updates as a stream" model the live
// WebSocket path uses (see useFlightStore.upsertFlight) — just driven by
// scrub position instead of real time, since GET /replay returns a sparse
// per-aircraft update stream rather than synchronized frames.
//
// sampleTimesMs is samples' recorded_at fields pre-parsed to epoch ms
// (same index, computed once in useReplayStore.loadWindow), since this
// runs on every scrub tick / playback frame and re-parsing each sample's
// date string that often is unnecessary work.
export function computeFlightsAtTime(
  samples: ReplaySample[],
  sampleTimesMs: number[],
  atMs: number | null,
): FlightState[] {
  if (atMs === null) return [];

  const latest = new Map<string, ReplaySample>();
  for (let i = 0; i < samples.length; i++) {
    // Samples are time-ascending (see GET /replay's ordering guarantee),
    // so once one is past atMs every subsequent sample is too.
    if (sampleTimesMs[i] > atMs) break;
    latest.set(samples[i].icao24, samples[i]);
  }
  return Array.from(latest.values()).map(sampleToFlightState);
}

// sampleToFlightState fills in the canonical FlightState fields a replay
// sample doesn't carry (flight_history is downsampled to position/motion
// fields only — see docs/architecture/data-model.md) so replay frames can
// reuse the same map rendering path as live FlightState data.
function sampleToFlightState(sample: ReplaySample): FlightState {
  return {
    icao24: sample.icao24,
    callsign: null,
    registration: null,
    lat: sample.lat,
    lon: sample.lon,
    altitude_baro_ft: sample.altitude_baro_ft,
    altitude_geo_ft: null,
    ground_speed_kt: sample.ground_speed_kt,
    vertical_rate_fpm: null,
    heading_deg: sample.heading_deg,
    on_ground: sample.on_ground,
    squawk: null,
    sources: [],
    position_quality: "adsb",
    last_seen_utc: sample.recorded_at,
    stale: false,
    // Downsampled replay history carries no type metadata.
    aircraft_type: null,
    emitter_category: null,
    military: false,
    icon_class: null,
  };
}
