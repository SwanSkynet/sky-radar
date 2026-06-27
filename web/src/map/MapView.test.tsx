import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import type { FlightSocketHandlers } from "../api/ws";
import type { FlightState } from "../api/flights";

// vi.mock factories are hoisted above the rest of the module, so the fake
// map class has to live inside the factory; vi.hoisted lets the test still
// reach into its instances afterward to manually fire "moveend".
const { fakeMapInstances } = vi.hoisted(() => ({
  fakeMapInstances: [] as { emit: (event: string) => void }[],
}));

// jsdom has no WebGL context, so the real maplibre-gl/@deck.gl/mapbox
// would throw on construction. Fake just enough of their surface for
// MapView to mount and drive a (re)subscribe.
vi.mock("maplibre-gl", () => {
  class FakeLngLatBounds {
    getWest() {
      return -10;
    }
    getEast() {
      return 10;
    }
    getSouth() {
      return -10;
    }
    getNorth() {
      return 10;
    }
  }
  class FakeMap {
    private handlers: Record<string, (() => void)[]> = {};
    constructor() {
      fakeMapInstances.push(
        this as unknown as { emit: (event: string) => void },
      );
    }
    on(event: string, handler: () => void) {
      (this.handlers[event] ??= []).push(handler);
      if (event === "load") handler();
    }
    off(event: string, handler: () => void) {
      this.handlers[event] = (this.handlers[event] ?? []).filter(
        (h) => h !== handler,
      );
    }
    emit(event: string) {
      for (const handler of this.handlers[event] ?? []) handler();
    }
    addControl() {}
    getBounds() {
      return new FakeLngLatBounds();
    }
    remove() {}
  }
  return { default: { Map: FakeMap } };
});

vi.mock("@deck.gl/mapbox", () => {
  class FakeMapboxOverlay {
    setProps() {}
  }
  return { MapboxOverlay: FakeMapboxOverlay };
});

interface FakeFlightSocket {
  bbox: [number, number, number, number];
  handlers: FlightSocketHandlers;
  closed: boolean;
  updateBBox: (bbox: [number, number, number, number]) => void;
  close: () => void;
}

// vi.mock factories are hoisted above the rest of the module, so the fake
// class has to live inside the factory; vi.hoisted lets the test still
// reach into its instances afterward.
const { fakeSocketInstances } = vi.hoisted(() => ({
  fakeSocketInstances: [] as FakeFlightSocket[],
}));

vi.mock("../api/ws", () => {
  class FakeFlightSocket {
    bbox: [number, number, number, number];
    handlers: FlightSocketHandlers;
    closed = false;

    constructor(
      bbox: [number, number, number, number],
      handlers: FlightSocketHandlers,
    ) {
      this.bbox = bbox;
      this.handlers = handlers;
      fakeSocketInstances.push(this);
    }

    updateBBox(bbox: [number, number, number, number]) {
      this.bbox = bbox;
    }

    close() {
      this.closed = true;
    }
  }
  return { FlightSocket: FakeFlightSocket };
});

import { MapView } from "./MapView";
import { useFlightStore } from "../store/useFlightStore";
import { useReplayStore } from "../store/useReplayStore";

function sampleFlight(overrides: Partial<FlightState> = {}): FlightState {
  return {
    icao24: "abc123",
    callsign: null,
    registration: null,
    lat: 1,
    lon: 2,
    altitude_baro_ft: null,
    altitude_geo_ft: null,
    ground_speed_kt: null,
    vertical_rate_fpm: null,
    heading_deg: null,
    on_ground: false,
    squawk: null,
    sources: [],
    position_quality: "adsb",
    last_seen_utc: new Date().toISOString(),
    stale: false,
    aircraft_type: null,
    emitter_category: null,
    military: false,
    icon_class: null,
    ...overrides,
  };
}

describe("MapView", () => {
  beforeEach(() => {
    fakeSocketInstances.length = 0;
    fakeMapInstances.length = 0;
    useFlightStore.setState({
      flights: {},
      connectionStatus: "connecting",
      lastUpdated: null,
      error: null,
    });
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

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("subscribes over WebSocket with the current viewport bbox, not REST polling", async () => {
    render(<MapView />);

    await waitFor(() => expect(fakeSocketInstances.length).toBe(1));
    expect(fakeSocketInstances[0].bbox).toEqual([-10, -10, 10, 10]);
  });

  it("renders an aircraft pushed via a flight_update handler", async () => {
    render(<MapView />);
    await waitFor(() => expect(fakeSocketInstances.length).toBe(1));

    fakeSocketInstances[0].handlers.onFlightUpdate(sampleFlight());

    await waitFor(() => {
      expect(screen.getByText("1 aircraft in view")).toBeInTheDocument();
    });
  });

  it("shows a degraded-mode banner while reconnecting, distinct from per-aircraft staleness", async () => {
    render(<MapView />);
    await waitFor(() => expect(fakeSocketInstances.length).toBe(1));

    fakeSocketInstances[0].handlers.onStatusChange("reconnecting");

    await waitFor(() => {
      expect(screen.getByText(/reconnecting/i)).toBeInTheDocument();
    });
  });

  it("falls back to a REST reload when the gateway reports a failed resume", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => [sampleFlight({ icao24: "fallback" })],
      }),
    );

    render(<MapView />);
    await waitFor(() => expect(fakeSocketInstances.length).toBe(1));

    fakeSocketInstances[0].handlers.onResumeFailed("resume gap too large");

    await waitFor(() => {
      expect(fetch).toHaveBeenCalled();
    });
    const calledUrl = (fetch as ReturnType<typeof vi.fn>).mock
      .calls[0][0] as string;
    expect(calledUrl).toContain("/flights?bbox=");
  });

  it("ignores a stale REST reload response once the viewport changes again", async () => {
    let resolveFetch!: (value: {
      ok: boolean;
      json: () => Promise<FlightState[]>;
    }) => void;
    let capturedSignal: AbortSignal | undefined;
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation((_url: string, init?: RequestInit) => {
        capturedSignal = init?.signal as AbortSignal | undefined;
        return new Promise((resolve) => {
          resolveFetch = resolve;
        });
      }),
    );

    render(<MapView />);
    await waitFor(() => expect(fakeSocketInstances.length).toBe(1));

    fakeSocketInstances[0].handlers.onResumeFailed("resume gap too large");
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));

    // A new viewport update (or unmount) arrives before the stale REST
    // request resolves; it should be aborted rather than allowed to
    // overwrite the WebSocket-backed state once it does resolve.
    fakeMapInstances[0].emit("moveend");
    expect(capturedSignal?.aborted).toBe(true);

    resolveFetch({
      ok: true,
      json: async () => [sampleFlight({ icao24: "stale" })],
    });
    await Promise.resolve();
    await Promise.resolve();

    expect(useFlightStore.getState().flights).toEqual({});
  });

  it("aborts an in-flight replay fetch when the user exits replay mode before it resolves", async () => {
    let resolveFetch!: (value: {
      ok: boolean;
      json: () => Promise<unknown[]>;
    }) => void;
    let capturedSignal: AbortSignal | undefined;
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation((_url: string, init?: RequestInit) => {
        capturedSignal = init?.signal as AbortSignal | undefined;
        return new Promise((resolve) => {
          resolveFetch = resolve;
        });
      }),
    );

    render(<MapView />);
    await waitFor(() => expect(fakeSocketInstances.length).toBe(1));

    fireEvent.click(screen.getByText("replay last 30 min"));
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));
    expect(capturedSignal?.aborted).toBe(false);

    // The user exits replay before the GET /replay response arrives; the
    // request should be aborted rather than left to land later and
    // resurrect replay state the user already dismissed.
    fireEvent.click(screen.getByText("exit replay"));
    expect(capturedSignal?.aborted).toBe(true);

    resolveFetch({ ok: true, json: async () => [] });
    await Promise.resolve();
    await Promise.resolve();

    const state = useReplayStore.getState();
    expect(state.isActive).toBe(false);
    expect(state.error).toBeNull();
  });
});
