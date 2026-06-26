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
import { useFlightStore } from "../store/useFlightStore";

// Frontend polls REST per docs/prd/phase-1-foundation.md (WebSocket push is
// out of scope for phase 1); definition of done asks for updates at least
// every 10-15s.
const POLL_INTERVAL_MS = 10_000;

// Free, no-API-key basemap style, consistent with the no-revenue-model
// constraint in docs/decisions/0004-map-rendering-maplibre-deckgl.md.
const MAP_STYLE_URL = "https://basemaps.cartocdn.com/gl/dark-matter-gl-style/style.json";

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

// MapView renders the canonical FlightState positions returned by GET
// /flights on a MapLibre base map via a deck.gl overlay, polling on an
// interval and on viewport change. See docs/architecture/data-model.md
// for the wire shape and docs/tech-stack/frontend.md for the stack.
export function MapView() {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const overlayRef = useRef<MapboxOverlay | null>(null);

  const flights = useFlightStore((s) => s.flights);
  const lastUpdated = useFlightStore((s) => s.lastUpdated);
  const error = useFlightStore((s) => s.error);
  const setFlights = useFlightStore((s) => s.setFlights);
  const setError = useFlightStore((s) => s.setError);

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

    let cancelled = false;
    let abortController: AbortController | null = null;

    const poll = () => {
      const bbox = clampBBox(map.getBounds());
      if (!bbox) return;

      abortController?.abort();
      abortController = new AbortController();

      fetchFlightsByBBox(bbox, abortController.signal)
        .then((data) => {
          if (!cancelled) setFlights(data);
        })
        .catch((err: unknown) => {
          if (
            cancelled ||
            (err instanceof DOMException && err.name === "AbortError")
          ) {
            return;
          }
          setError(
            err instanceof Error ? err.message : "failed to load flights",
          );
        });
    };

    map.on("load", poll);
    map.on("moveend", poll);
    const interval = setInterval(poll, POLL_INTERVAL_MS);

    return () => {
      cancelled = true;
      abortController?.abort();
      clearInterval(interval);
      overlayRef.current = null;
      map.remove();
    };
  }, [setFlights, setError]);

  useEffect(() => {
    overlayRef.current?.setProps({ layers: [buildAircraftLayer(flights)] });
  }, [flights]);

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
        <div>{flights.length} aircraft in view</div>
        {lastUpdated && (
          <div className="text-xs">
            updated {lastUpdated.toLocaleTimeString()}
          </div>
        )}
        {error && <div className="text-xs text-red-500">{error}</div>}
      </div>
    </div>
  );
}
