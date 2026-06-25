import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, waitFor } from "@testing-library/react";

// jsdom has no WebGL context, so the real maplibre-gl/@deck.gl/mapbox
// would throw on construction. Fake just enough of their surface for
// MapView to mount and drive a poll.
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
    on(event: string, handler: () => void) {
      if (event === "load") handler();
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

import { MapView } from "./MapView";

describe("MapView", () => {
  beforeEach(() => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => [],
      }),
    );
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("mounts the map and issues a flights request", async () => {
    render(<MapView />);

    await waitFor(() => {
      expect(fetch).toHaveBeenCalled();
    });

    const calledUrl = (fetch as ReturnType<typeof vi.fn>).mock.calls[0][0] as string;
    expect(calledUrl).toContain("/flights?bbox=");
  });
});
