import { useEffect, useRef } from "react";
import maplibregl from "maplibre-gl";
import "maplibre-gl/dist/maplibre-gl.css";
import { MapboxOverlay } from "@deck.gl/mapbox";
import { ScatterplotLayer } from "@deck.gl/layers";
import {
  fetchFlightsByBBox,
  type BBox,
  type FlightState,
} from "../api/flights";
import { FlightSocket, type ConnectionStatus } from "../api/ws";
import { useFlightStore } from "../store/useFlightStore";

// How often per-aircraft staleness is recomputed against the wall clock
// (see useFlightStore.recomputeStaleness) — the WS push path only updates
// an aircraft's fields when a new message for it arrives, so staleness
// has to be re-derived independently of message traffic.
const STALE_RECOMPUTE_INTERVAL_MS = 5_000;

// Free, no-API-key basemap style, consistent with the no-revenue-model
// constraint in docs/decisions/0004-map-rendering-maplibre-deckgl.md.
const MAP_STYLE_URL =
  "https://basemaps.cartocdn.com/gl/dark-matter-gl-style/style.json";

const STALE_COLOR: [number, number, number, number] = [148, 148, 158, 180];
const FRESH_COLOR: [number, number, number, number] = [56, 189, 248, 220];

function clampBBox(bounds: maplibregl.LngLatBounds): BBox | null {
  const minLon = Math.max(-180, bounds.getWest());
  const maxLon = Math.min(180, bounds.getEast());
  const minLat = Math.max(-90, bounds.getSouth());
  const maxLat = Math.min(90, bounds.getNorth());
  if (minLon >= maxLon || minLat >= maxLat) return null;
  return { minLon, minLat, maxLon, maxLat };
}

function bboxToTuple(bbox: BBox): [number, number, number, number] {
  return [bbox.minLon, bbox.minLat, bbox.maxLon, bbox.maxLat];
}

function buildAircraftLayer(
  flights: FlightState[],
): ScatterplotLayer<FlightState> {
  return new ScatterplotLayer<FlightState>({
    id: "aircraft",
    data: flights,
    getPosition: (d) => [d.lon, d.lat],
    getFillColor: (d) => (d.stale ? STALE_COLOR : FRESH_COLOR),
    getRadius: 6000,
    radiusUnits: "meters",
    radiusMinPixels: 3,
    radiusMaxPixels: 8,
    pickable: false,
  });
}

function connectionStatusLabel(status: ConnectionStatus): string | null {
  switch (status) {
    case "connecting":
      return "connecting to live feed…";
    case "reconnecting":
      return "live connection lost — reconnecting…";
    case "closed":
      return "disconnected";
    case "open":
      return null;
  }
}

// MapView renders canonical FlightState positions pushed over the
// gateway's WebSocket viewport subscription (see docs/architecture/
// system-architecture.md and cmd/apigateway/ws_protocol.go) on a MapLibre
// base map via a deck.gl overlay. The map (re)subscribes on load and on
// every viewport change; GET /flights is used only as a one-shot recovery
// fallback when the gateway reports a resume gap it can't replay.
export function MapView() {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const overlayRef = useRef<MapboxOverlay | null>(null);

  const flights = useFlightStore((s) => s.flights);
  const lastUpdated = useFlightStore((s) => s.lastUpdated);
  const error = useFlightStore((s) => s.error);
  const connectionStatus = useFlightStore((s) => s.connectionStatus);
  const upsertFlight = useFlightStore((s) => s.upsertFlight);
  const setFlights = useFlightStore((s) => s.setFlights);
  const retainWithinBBox = useFlightStore((s) => s.retainWithinBBox);
  const recomputeStaleness = useFlightStore((s) => s.recomputeStaleness);
  const setConnectionStatus = useFlightStore((s) => s.setConnectionStatus);
  const setError = useFlightStore((s) => s.setError);

  const flightList = Object.values(flights);

  useEffect(() => {
    if (!containerRef.current) return;

    const map = new maplibregl.Map({
      container: containerRef.current,
      style: MAP_STYLE_URL,
      center: [0, 20],
      zoom: 2,
    });

    const overlay = new MapboxOverlay({ interleaved: true, layers: [] });
    overlayRef.current = overlay;
    map.addControl(overlay as unknown as maplibregl.IControl);

    let socket: FlightSocket | null = null;

    const reloadFromREST = () => {
      const bbox = clampBBox(map.getBounds());
      if (!bbox) return;
      fetchFlightsByBBox(bbox)
        .then((data) => setFlights(data))
        .catch((err: unknown) => {
          setError(
            err instanceof Error ? err.message : "failed to reload flights",
          );
        });
    };

    const resubscribe = () => {
      const bbox = clampBBox(map.getBounds());
      if (!bbox) return;

      if (socket) {
        socket.updateBBox(bboxToTuple(bbox));
        retainWithinBBox(bbox);
        return;
      }

      socket = new FlightSocket(bboxToTuple(bbox), {
        onFlightUpdate: upsertFlight,
        onResumeFailed: reloadFromREST,
        onStatusChange: setConnectionStatus,
      });
    };

    map.on("load", resubscribe);
    map.on("moveend", resubscribe);
    const staleTimer = setInterval(
      recomputeStaleness,
      STALE_RECOMPUTE_INTERVAL_MS,
    );

    return () => {
      map.off("load", resubscribe);
      map.off("moveend", resubscribe);
      clearInterval(staleTimer);
      socket?.close();
      overlayRef.current = null;
      map.remove();
    };
  }, [
    setFlights,
    setError,
    upsertFlight,
    retainWithinBBox,
    recomputeStaleness,
    setConnectionStatus,
  ]);

  useEffect(() => {
    overlayRef.current?.setProps({ layers: [buildAircraftLayer(flightList)] });
  }, [flightList]);

  const statusLabel = connectionStatusLabel(connectionStatus);

  return (
    <div className="relative h-full min-h-svh w-full flex-1">
      {/* Inline styles, not Tailwind's `absolute inset-0`: maplibre-gl.css
          sets `.maplibregl-map { position: relative }` on this same node
          (MapLibre adds that class to the container we pass it), and since
          that stylesheet loads after Tailwind's, it wins the cascade tie
          and silently collapses this div to height 0. Inline styles always
          beat class-based rules regardless of load order. */}
      <div ref={containerRef} style={{ position: "absolute", inset: 0 }} />
      <div className="absolute top-3 left-3 z-10 rounded bg-bg/90 px-3 py-2 text-left text-sm text-text shadow">
        <div>{flightList.length} aircraft in view</div>
        {lastUpdated && (
          <div className="text-xs">
            updated {lastUpdated.toLocaleTimeString()}
          </div>
        )}
        {error && <div className="text-xs text-red-500">{error}</div>}
      </div>
      {statusLabel && (
        <div className="absolute top-3 right-3 z-10 rounded bg-amber-900/90 px-3 py-2 text-left text-sm text-amber-100 shadow">
          {statusLabel}
        </div>
      )}
    </div>
  );
}
