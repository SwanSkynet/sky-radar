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
  // recorded_at parsed to epoch ms once per sample at load time, parallel
  // to `samples` by index. computeFlightsAtTime runs on every scrub tick
  // (including every playback frame), so this avoids re-parsing the same
  // date strings dozens of times per second.
  sampleTimesMs: number[];
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
  sampleTimesMs: [],
  windowStartMs: null,
  windowEndMs: null,
  scrubMs: null,

  startLoading: () => set({ isActive: true, isLoading: true, error: null }),

  loadWindow: (samples, windowStartMs, windowEndMs) => {
    // Drop any sample whose recorded_at doesn't parse to a finite
    // timestamp instead of caching NaN into sampleTimesMs — NaN
    // comparisons in computeFlightsAtTime's cutoff check are always
    // false, so a malformed sample would never be excluded by the scrub
    // position. samples and sampleTimesMs must stay aligned by index.
    const validSamples: ReplaySample[] = [];
    const validTimesMs: number[] = [];
    for (const s of samples) {
      const t = new Date(s.recorded_at).getTime();
      if (Number.isFinite(t)) {
        validSamples.push(s);
        validTimesMs.push(t);
      }
    }
    set({
      // Keep the original array reference when nothing was dropped.
      samples: validSamples.length === samples.length ? samples : validSamples,
      sampleTimesMs: validTimesMs,
      windowStartMs,
      windowEndMs,
      scrubMs: windowStartMs,
      isLoading: false,
      isPlaying: false,
      error: null,
    });
  },

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
      if (
        !state.isPlaying &&
        state.scrubMs !== null &&
        state.scrubMs >= state.windowEndMs
      ) {
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
      sampleTimesMs: [],
      windowStartMs: null,
      windowEndMs: null,
      scrubMs: null,
    }),
}));
