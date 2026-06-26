import { create } from "zustand";
import type { ReplaySample } from "../api/replay";

interface ReplayStore {
  // Whether replay mode is showing at all — distinct from isLoading/error,
  // which can be true/set while isActive is already true (e.g. re-fetching
  // a new window without first dropping back to live mode).
  isActive: boolean;
  isLoading: boolean;
  isPlaying: boolean;
  error: string | null;
  samples: ReplaySample[];
  windowStartMs: number | null;
  windowEndMs: number | null;
  scrubMs: number | null;

  startLoading: () => void;
  loadWindow: (
    samples: ReplaySample[],
    windowStartMs: number,
    windowEndMs: number,
  ) => void;
  setError: (error: string) => void;
  setScrub: (ms: number) => void;
  togglePlay: () => void;
  pause: () => void;
  exit: () => void;
}

export const useReplayStore = create<ReplayStore>((set) => ({
  isActive: false,
  isLoading: false,
  isPlaying: false,
  error: null,
  samples: [],
  windowStartMs: null,
  windowEndMs: null,
  scrubMs: null,

  startLoading: () => set({ isActive: true, isLoading: true, error: null }),

  loadWindow: (samples, windowStartMs, windowEndMs) =>
    set({
      samples,
      windowStartMs,
      windowEndMs,
      scrubMs: windowStartMs,
      isLoading: false,
      isPlaying: false,
      error: null,
    }),

  setError: (error) => set({ isLoading: false, error }),

  // Clamped to the loaded window so a playback tick or a dragged slider
  // can't push the scrub head past what was actually fetched.
  setScrub: (ms) =>
    set((state) => {
      if (state.windowStartMs === null || state.windowEndMs === null) {
        return {};
      }
      const clamped = Math.min(
        Math.max(ms, state.windowStartMs),
        state.windowEndMs,
      );
      return {
        scrubMs: clamped,
        // Stop auto-play once playback reaches the end of the window
        // instead of sitting at the end flagged "playing" forever.
        isPlaying: state.isPlaying && clamped < state.windowEndMs,
      };
    }),

  togglePlay: () =>
    set((state) => {
      if (state.windowStartMs === null || state.windowEndMs === null) {
        return {};
      }
      if (!state.isPlaying && state.scrubMs !== null && state.scrubMs >= state.windowEndMs) {
        // Pressing play after reaching the end restarts from the beginning
        // rather than doing nothing (the head is already clamped at end).
        return { isPlaying: true, scrubMs: state.windowStartMs };
      }
      return { isPlaying: !state.isPlaying };
    }),

  pause: () => set({ isPlaying: false }),

  exit: () =>
    set({
      isActive: false,
      isLoading: false,
      isPlaying: false,
      error: null,
      samples: [],
      windowStartMs: null,
      windowEndMs: null,
      scrubMs: null,
    }),
}));
