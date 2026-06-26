import { create } from "zustand";
import type { BBox, FlightState } from "../api/flights";
import type { ConnectionStatus } from "../api/ws";

// Mirrors internal/flightmodel/staleness.go's StaleThreshold. The WS push
// path (unlike GET /flights) never recomputes `stale` server-side once an
// aircraft stops sending updates, so the client recomputes it locally on a
// timer (see MapView) instead of trusting whatever `stale` value rode in
// on the last message for that aircraft.
export const STALE_THRESHOLD_MS = 60_000;

interface FlightStore {
  // Keyed by icao24 so single-aircraft WS pushes can be applied as
  // incremental upserts instead of replacing the whole viewport on every
  // message, the way the old poll-and-replace REST flow did.
  flights: Record<string, FlightState>;
  // Per-connection health of the realtime path itself — distinct from any
  // single aircraft's `stale` flag, this is what should drive a degraded-
  // mode banner (see docs/tech-stack/frontend.md#degraded-mode-ui).
  connectionStatus: ConnectionStatus;
  lastUpdated: Date | null;
  error: string | null;
  upsertFlight: (flight: FlightState) => void;
  setFlights: (flights: FlightState[]) => void;
  // Drops tracked aircraft outside bbox, e.g. after the user pans/zooms
  // and the server-side viewport filter has moved on from them too.
  retainWithinBBox: (bbox: BBox) => void;
  recomputeStaleness: () => void;
  setConnectionStatus: (status: ConnectionStatus) => void;
  setError: (error: string | null) => void;
}

export const useFlightStore = create<FlightStore>((set) => ({
  flights: {},
  connectionStatus: "connecting",
  lastUpdated: null,
  error: null,

  upsertFlight: (flight) =>
    set((state) => ({
      flights: { ...state.flights, [flight.icao24]: flight },
      lastUpdated: new Date(),
      error: null,
    })),

  setFlights: (flights) =>
    set({
      flights: Object.fromEntries(flights.map((f) => [f.icao24, f])),
      lastUpdated: new Date(),
      error: null,
    }),

  retainWithinBBox: (bbox) =>
    set((state) => ({
      flights: Object.fromEntries(
        Object.entries(state.flights).filter(
          ([, f]) =>
            f.lon >= bbox.minLon &&
            f.lon <= bbox.maxLon &&
            f.lat >= bbox.minLat &&
            f.lat <= bbox.maxLat,
        ),
      ),
    })),

  recomputeStaleness: () =>
    set((state) => {
      const now = Date.now();
      let changed = false;
      const flights = { ...state.flights };
      for (const [icao24, flight] of Object.entries(flights)) {
        const stale =
          now - new Date(flight.last_seen_utc).getTime() > STALE_THRESHOLD_MS;
        if (flight.stale !== stale) {
          flights[icao24] = { ...flight, stale };
          changed = true;
        }
      }
      return changed ? { flights } : {};
    }),

  setConnectionStatus: (connectionStatus) => set({ connectionStatus }),
  setError: (error) => set({ error }),
}));
