// Pure formatting helpers for the flight detail drawer, kept separate from
// the React component so they can be unit-tested directly.

import type { FlightState } from "../api/flights";

// Emergency squawk codes decoded for the status row (per the decided field
// set in docs/features/batch-1-coverage-detail-icons-cadence.md, Feature 1).
const EMERGENCY_SQUAWKS: Record<string, string> = {
  "7500": "hijack",
  "7600": "radio failure",
  "7700": "general emergency",
};

// decodeSquawkEmergency returns a human-readable emergency label for the
// reserved squawk codes 7500/7600/7700, or null for any normal code.
export function decodeSquawkEmergency(squawk: string | null): string | null {
  if (!squawk) return null;
  return EMERGENCY_SQUAWKS[squawk.trim()] ?? null;
}

// headerLabel is the callsign if present, falling back to the ICAO24 hex so
// the drawer header is never empty.
export function headerLabel(flight: FlightState): string {
  const callsign = flight.callsign?.trim();
  return callsign && callsign.length > 0
    ? callsign
    : flight.icao24.toUpperCase();
}

// verticalRateArrow returns a climb/descend indicator for a vertical rate in
// feet per minute, or "" when level/unknown (within a small deadband so a
// near-zero rate doesn't flicker an arrow).
export function verticalRateArrow(fpm: number | null): string {
  if (fpm == null) return "";
  if (fpm > 100) return "▲";
  if (fpm < -100) return "▼";
  return "";
}

// secondsAgo returns whole seconds between lastSeenUtc and now (clamped at 0),
// for the "last seen N s ago" provenance row.
export function secondsAgo(
  lastSeenUtc: string,
  now: number = Date.now(),
): number {
  const seen = new Date(lastSeenUtc).getTime();
  if (Number.isNaN(seen)) return 0;
  // Floor (not round) so the displayed age stays monotonic and never jumps
  // ahead of real elapsed time (e.g. "2 s ago" after only 1.5 s).
  return Math.max(0, Math.floor((now - seen) / 1000));
}

// aircraftTypeLabel combines the raw ICAO designator with the classified icon
// bucket, e.g. "A320 · commercial jet", or null when no type is known.
export function aircraftTypeLabel(flight: FlightState): string | null {
  const designator = flight.aircraft_type?.trim();
  const bucket = flight.icon_class?.replace(/_/g, " ");
  if (designator && bucket) return `${designator} · ${bucket}`;
  if (designator) return designator;
  if (bucket) return bucket;
  return null;
}

// positionQualityLabel maps the enum to a friendly label.
export function positionQualityLabel(
  q: FlightState["position_quality"],
): string {
  switch (q) {
    case "adsb":
      return "ADS-B";
    case "mlat":
      return "MLAT";
    case "estimated":
      return "estimated";
    default:
      return q;
  }
}
