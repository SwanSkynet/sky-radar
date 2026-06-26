import { describe, it, expect, beforeEach } from "vitest";
import { useReplayStore } from "./useReplayStore";
import type { ReplaySample } from "../api/replay";

function sample(overrides: Partial<ReplaySample> = {}): ReplaySample {
  return {
    icao24: "abc123",
    recorded_at: new Date(0).toISOString(),
    lat: 1,
    lon: 2,
    altitude_baro_ft: null,
    ground_speed_kt: null,
    heading_deg: null,
    on_ground: false,
    ...overrides,
  };
}

describe("useReplayStore", () => {
  beforeEach(() => {
    useReplayStore.setState({
      isActive: false,
      isLoading: false,
      isPlaying: false,
      error: null,
      samples: [],
      sampleTimesMs: [],
      windowStartMs: null,
      windowEndMs: null,
      scrubMs: null,
    });
  });

  it("startLoading enters replay mode in a loading state with no stale error", () => {
    useReplayStore.setState({ error: "previous error" });
    useReplayStore.getState().startLoading();

    const state = useReplayStore.getState();
    expect(state.isActive).toBe(true);
    expect(state.isLoading).toBe(true);
    expect(state.error).toBeNull();
  });

  it("loadWindow stores the samples and positions the scrub head at the window start", () => {
    const samples = [sample()];
    useReplayStore.getState().loadWindow(samples, 1000, 5000);

    const state = useReplayStore.getState();
    expect(state.samples).toBe(samples);
    expect(state.windowStartMs).toBe(1000);
    expect(state.windowEndMs).toBe(5000);
    expect(state.scrubMs).toBe(1000);
    expect(state.isLoading).toBe(false);
  });

  it("loadWindow precomputes each sample's recorded_at as epoch ms, parallel to samples", () => {
    const samples = [
      sample({ recorded_at: new Date(1000).toISOString() }),
      sample({ recorded_at: new Date(2000).toISOString() }),
    ];
    useReplayStore.getState().loadWindow(samples, 1000, 5000);

    expect(useReplayStore.getState().sampleTimesMs).toEqual([1000, 2000]);
  });

  it("setError surfaces the error and clears the loading flag", () => {
    useReplayStore.getState().startLoading();
    useReplayStore.getState().setError("failed to load replay window");

    const state = useReplayStore.getState();
    expect(state.error).toBe("failed to load replay window");
    expect(state.isLoading).toBe(false);
  });

  it("setScrub clamps to the loaded window bounds", () => {
    useReplayStore.getState().loadWindow([], 1000, 5000);

    useReplayStore.getState().setScrub(-100);
    expect(useReplayStore.getState().scrubMs).toBe(1000);

    useReplayStore.getState().setScrub(9999);
    expect(useReplayStore.getState().scrubMs).toBe(5000);

    useReplayStore.getState().setScrub(3000);
    expect(useReplayStore.getState().scrubMs).toBe(3000);
  });

  it("setScrub is a no-op when no window has been loaded", () => {
    useReplayStore.getState().setScrub(3000);
    expect(useReplayStore.getState().scrubMs).toBeNull();
  });

  it("togglePlay starts and stops playback", () => {
    useReplayStore.getState().loadWindow([], 1000, 5000);

    useReplayStore.getState().togglePlay();
    expect(useReplayStore.getState().isPlaying).toBe(true);

    useReplayStore.getState().togglePlay();
    expect(useReplayStore.getState().isPlaying).toBe(false);
  });

  it("togglePlay restarts from the beginning when pressed after reaching the end", () => {
    useReplayStore.getState().loadWindow([], 1000, 5000);
    useReplayStore.getState().setScrub(5000);

    useReplayStore.getState().togglePlay();

    const state = useReplayStore.getState();
    expect(state.isPlaying).toBe(true);
    expect(state.scrubMs).toBe(1000);
  });

  it("setScrub reaching the window end stops playback automatically", () => {
    useReplayStore.getState().loadWindow([], 1000, 5000);
    useReplayStore.getState().togglePlay();
    expect(useReplayStore.getState().isPlaying).toBe(true);

    useReplayStore.getState().setScrub(5000);
    expect(useReplayStore.getState().isPlaying).toBe(false);
  });

  it("exit resets to the initial live-mode state", () => {
    useReplayStore.getState().loadWindow([sample()], 1000, 5000);
    useReplayStore.getState().togglePlay();

    useReplayStore.getState().exit();

    const state = useReplayStore.getState();
    expect(state.isActive).toBe(false);
    expect(state.isPlaying).toBe(false);
    expect(state.samples).toEqual([]);
    expect(state.windowStartMs).toBeNull();
    expect(state.scrubMs).toBeNull();
  });
});
