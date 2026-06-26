import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { FlightSocket, type FlightSocketHandlers } from "./ws";

class FakeWebSocket {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSED = 3;
  static instances: FakeWebSocket[] = [];

  readyState = FakeWebSocket.CONNECTING;
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  sent: string[] = [];
  url: string;

  constructor(url: string) {
    this.url = url;
    FakeWebSocket.instances.push(this);
  }

  send(data: string) {
    this.sent.push(data);
  }

  close() {
    this.readyState = FakeWebSocket.CLOSED;
    this.onclose?.();
  }

  triggerOpen() {
    this.readyState = FakeWebSocket.OPEN;
    this.onopen?.();
  }

  triggerMessage(payload: unknown) {
    this.onmessage?.({ data: JSON.stringify(payload) });
  }
}

function handlers(): FlightSocketHandlers {
  return {
    onFlightUpdate: vi.fn(),
    onResumeFailed: vi.fn(),
    onStatusChange: vi.fn(),
  };
}

describe("FlightSocket", () => {
  beforeEach(() => {
    FakeWebSocket.instances = [];
    vi.stubGlobal("WebSocket", FakeWebSocket as unknown as typeof WebSocket);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  it("sends a subscribe message with the bbox once the socket opens", () => {
    const h = handlers();
    new FlightSocket([-10, -10, 10, 10], h);

    const ws = FakeWebSocket.instances[0];
    ws.triggerOpen();

    expect(JSON.parse(ws.sent[0])).toEqual({
      type: "subscribe",
      bbox: [-10, -10, 10, 10],
    });
    expect(h.onStatusChange).toHaveBeenCalledWith("connecting");
  });

  it("dispatches flight_update messages and tracks the sequence", () => {
    const h = handlers();
    new FlightSocket([-10, -10, 10, 10], h);
    const ws = FakeWebSocket.instances[0];
    ws.triggerOpen();

    const state = { icao24: "abc123", lat: 1, lon: 2 };
    ws.triggerMessage({ type: "flight_update", seq: 5, state });

    expect(h.onFlightUpdate).toHaveBeenCalledWith(state);
  });

  it("notifies onResumeFailed and continues without closing the connection", () => {
    const h = handlers();
    new FlightSocket([-10, -10, 10, 10], h);
    const ws = FakeWebSocket.instances[0];
    ws.triggerOpen();

    ws.triggerMessage({ type: "resume_failed", reason: "gap too large" });

    expect(h.onResumeFailed).toHaveBeenCalledWith("gap too large");
    expect(ws.readyState).not.toBe(FakeWebSocket.CLOSED);
  });

  it("clears the tracked sequence on resume_failed so the next reconnect starts fresh", () => {
    vi.useFakeTimers();
    const h = handlers();
    new FlightSocket([-10, -10, 10, 10], h);

    const ws1 = FakeWebSocket.instances[0];
    ws1.triggerOpen();
    ws1.triggerMessage({ type: "subscribed", seq: 7 });
    ws1.triggerMessage({ type: "resume_failed", reason: "gap too large" });

    ws1.close();
    vi.advanceTimersByTime(1000);

    const ws2 = FakeWebSocket.instances[1];
    ws2.triggerOpen();

    expect(JSON.parse(ws2.sent[0])).toEqual({
      type: "subscribe",
      bbox: [-10, -10, 10, 10],
    });
  });

  it("re-subscribes with resume_from_seq after reconnecting", () => {
    vi.useFakeTimers();
    const h = handlers();
    new FlightSocket([-10, -10, 10, 10], h);

    const ws1 = FakeWebSocket.instances[0];
    ws1.triggerOpen();
    ws1.triggerMessage({ type: "subscribed", seq: 7 });

    ws1.close();
    expect(h.onStatusChange).toHaveBeenCalledWith("reconnecting");

    vi.advanceTimersByTime(1000);

    const ws2 = FakeWebSocket.instances[1];
    ws2.triggerOpen();

    expect(JSON.parse(ws2.sent[0])).toEqual({
      type: "subscribe",
      bbox: [-10, -10, 10, 10],
      resume_from_seq: 7,
    });
  });

  it("sends an updated bbox over the open socket without reconnecting", () => {
    const h = handlers();
    const socket = new FlightSocket([-10, -10, 10, 10], h);
    const ws = FakeWebSocket.instances[0];
    ws.triggerOpen();

    socket.updateBBox([0, 0, 5, 5]);

    expect(JSON.parse(ws.sent[1])).toEqual({
      type: "subscribe",
      bbox: [0, 0, 5, 5],
    });
    expect(FakeWebSocket.instances.length).toBe(1);
  });

  it("stops reconnecting once closed by the caller", () => {
    vi.useFakeTimers();
    const h = handlers();
    const socket = new FlightSocket([-10, -10, 10, 10], h);
    const ws1 = FakeWebSocket.instances[0];
    ws1.triggerOpen();

    socket.close();
    expect(h.onStatusChange).toHaveBeenCalledWith("closed");

    vi.advanceTimersByTime(60_000);
    expect(FakeWebSocket.instances.length).toBe(1);
  });
});
