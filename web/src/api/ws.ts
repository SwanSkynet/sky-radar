// Thin reconnect/backoff wrapper around the native WebSocket API, speaking
// the subscribe/resume protocol defined in cmd/apigateway/ws_protocol.go.
// Per docs/tech-stack/frontend.md this is deliberately not a third-party
// real-time library — the gateway's contract is small enough that a native
// WebSocket plus backoff covers it.

import { API_BASE_URL } from "./flights";
import type { FlightState } from "./flights";

export type ConnectionStatus =
  | "connecting"
  | "open"
  | "reconnecting"
  | "closed";

type BBoxTuple = [number, number, number, number];

interface WSClientMessage {
  type: "subscribe";
  bbox: BBoxTuple;
  resume_from_seq?: number;
}

interface WSServerMessage {
  type: "subscribed" | "flight_update" | "resume_failed";
  seq?: number;
  state?: FlightState;
  reason?: string;
}

export interface FlightSocketHandlers {
  onFlightUpdate: (state: FlightState) => void;
  // Fired when the gateway can't replay a reconnect's missed sequence gap
  // (see ws_gateway.go's replayResume) — the caller must fall back to a
  // full state reload (e.g. GET /flights) since the WS stream alone no
  // longer has a complete picture of the current viewport.
  onResumeFailed: (reason: string) => void;
  onStatusChange: (status: ConnectionStatus) => void;
}

const RECONNECT_BASE_DELAY_MS = 1000;
const RECONNECT_MAX_DELAY_MS = 30_000;

function wsURL(): string {
  if (API_BASE_URL.startsWith("/")) {
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    return `${protocol}//${window.location.host}${API_BASE_URL}/ws`;
  }
  return `${API_BASE_URL.replace(/^http/, "ws")}/ws`;
}

// FlightSocket owns one logical subscription: it reconnects with backoff
// on drop, resumes from the last sequence it saw, and re-sends the current
// bbox as a "subscribe" on every (re)connect, matching the gateway's
// "subscribe doubles as (re)register viewport" contract.
export class FlightSocket {
  private readonly handlers: FlightSocketHandlers;
  private socket: WebSocket | null = null;
  private bbox: BBoxTuple;
  private lastSeq: number | undefined;
  private closedByCaller = false;
  private reconnectAttempt = 0;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;

  constructor(bbox: BBoxTuple, handlers: FlightSocketHandlers) {
    this.handlers = handlers;
    this.bbox = bbox;
    this.open();
  }

  private open() {
    this.handlers.onStatusChange(
      this.reconnectAttempt > 0 ? "reconnecting" : "connecting",
    );

    const socket = new WebSocket(wsURL());
    this.socket = socket;

    socket.onopen = () => {
      const msg: WSClientMessage = { type: "subscribe", bbox: this.bbox };
      if (this.lastSeq !== undefined) msg.resume_from_seq = this.lastSeq;
      socket.send(JSON.stringify(msg));
    };

    socket.onmessage = (event: MessageEvent) => {
      let msg: WSServerMessage;
      try {
        msg = JSON.parse(event.data as string) as WSServerMessage;
      } catch {
        return;
      }
      switch (msg.type) {
        case "subscribed":
          this.reconnectAttempt = 0;
          if (msg.seq !== undefined) this.lastSeq = msg.seq;
          this.handlers.onStatusChange("open");
          break;
        case "flight_update":
          if (msg.state) {
            if (msg.seq !== undefined) this.lastSeq = msg.seq;
            this.handlers.onFlightUpdate(msg.state);
          }
          break;
        case "resume_failed":
          // The gateway couldn't replay the gap, so the sequence we were
          // tracking is no longer a valid resume point — clear it before
          // the caller's reload fallback runs, otherwise the next
          // reconnect would retry the same dead resume_from_seq.
          this.lastSeq = undefined;
          this.handlers.onResumeFailed(msg.reason ?? "resume failed");
          break;
      }
    };

    socket.onclose = () => {
      if (this.closedByCaller) return;
      this.scheduleReconnect();
    };
    socket.onerror = () => socket.close();
  }

  private scheduleReconnect() {
    this.handlers.onStatusChange("reconnecting");
    const delay = Math.min(
      RECONNECT_BASE_DELAY_MS * 2 ** this.reconnectAttempt,
      RECONNECT_MAX_DELAY_MS,
    );
    this.reconnectAttempt += 1;
    this.reconnectTimer = setTimeout(() => {
      if (!this.closedByCaller) this.open();
    }, delay);
  }

  // updateBBox re-registers the viewport, e.g. on map pan/zoom. If the
  // socket isn't open yet, the new bbox is simply what the next (re)connect
  // subscribes with.
  updateBBox(bbox: BBoxTuple) {
    this.bbox = bbox;
    if (this.socket?.readyState === WebSocket.OPEN) {
      const msg: WSClientMessage = { type: "subscribe", bbox };
      this.socket.send(JSON.stringify(msg));
    }
  }

  close() {
    this.closedByCaller = true;
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
    this.socket?.close();
    this.handlers.onStatusChange("closed");
  }
}
