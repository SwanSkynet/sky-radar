import { create } from "zustand";
import type { FlightState } from "../api/flights";

interface FlightStore {
  flights: FlightState[];
  lastUpdated: Date | null;
  error: string | null;
  setFlights: (flights: FlightState[]) => void;
  setError: (error: string | null) => void;
}

// Holds current-viewport aircraft state polled from GET /flights. Kept
// minimal per phase-1 scope — no selection/filter/replay state yet (see
// docs/tech-stack/frontend.md).
export const useFlightStore = create<FlightStore>((set) => ({
  flights: [],
  lastUpdated: null,
  error: null,
  setFlights: (flights) =>
    set({ flights, lastUpdated: new Date(), error: null }),
  setError: (error) => set({ error }),
}));
