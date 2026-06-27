import { useEffect, useMemo, useRef } from "react";
import maplibregl from "maplibre-gl";
import "maplibre-gl/dist/maplibre-gl.css";
import { MapboxOverlay } from "@deck.gl/mapbox";
import { IconLayer, ScatterplotLayer } from "@deck.gl/layers";
import type { Layer } from "@deck.gl/core";
import { iconDefFor, headingToIconAngle } from "./aircraftIcons";
import {
  fetchFlightsByBBox,
  type BBox,
  type FlightState,
} from "../api/flights";
import { fetchReplayWindow } from "../api/replay";
import { FlightSocket, type ConnectionStatus } from "../api/ws";
import { useFlightStore } from "../store/useFlightStore";
import { useReplayStore } from "../store/useReplayStore";
import { computeFlightsAtTime } from "../replay/replayPlayback";
import { ReplayScrubber } from "../replay/ReplayScrubber";
import { FlightDetailDrawer } from "./FlightDetailDrawer";
import type { PickingInfo } from "@deck.gl/core";

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
// Amber, distinct from live's blue/gray palette, so replayed aircraft are
// visually unmistakable from live ones at a glance (P2-FR5).
const REPLAY_COLOR: [number, number, number, number] = [251, 191, 36, 220];

// REPLAY_WINDOW_MS matches the documented default full-resolution replay
// window (docs/tech-stack/data-and-messaging.md / natsutil.FlightsUpdatesMaxAge).
const REPLAY_WINDOW_MS = 30 * 60 * 1000;

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

const SELECTED_RING_COLOR: [number, number, number, number] = [255, 255, 255, 230];

// Base on-screen icon size in pixels; the selected aircraft is drawn a bit
// larger on top of the selection ring.
const ICON_SIZE_PX = 26;
const ICON_SIZE_SELECTED_PX = 34;

// buildAircraftIconLayer renders aircraft as heading-rotated, per-type SVG
// icons. Icons are masks (see aircraftIcons.IconDef), so getColor carries the
// existing fresh-blue / stale-grey / replay-amber semantics over from the old
// ScatterplotLayer.
function buildAircraftIconLayer(
  flights: FlightState[],
  isReplay: boolean,
  selectedIcao24: string | null,
): IconLayer<FlightState> {
  return new IconLayer<FlightState>({
    id: "aircraft",
    data: flights,
    getPosition: (d) => [d.lon, d.lat],
    getIcon: (d) => iconDefFor(d.icon_class),
    getAngle: (d) => headingToIconAngle(d.heading_deg),
    getColor: isReplay
      ? REPLAY_COLOR
      : (d) => (d.stale ? STALE_COLOR : FRESH_COLOR),
    getSize: (d) =>
      d.icao24 === selectedIcao24 ? ICON_SIZE_SELECTED_PX : ICON_SIZE_PX,
    sizeUnits: "pixels",
    // Selection only applies to the live layer; replayed aircraft aren't
    // clickable.
    pickable: !isReplay,
    updateTriggers: {
      getColor: isReplay,
      getSize: selectedIcao24,
    },
  });
}

// buildSelectionRing draws a ring under the selected live aircraft so the
// selection stays obvious beneath the icon. Returns an empty list when
// nothing is selected or in replay mode.
function buildSelectionRing(
  flights: FlightState[],
  selectedIcao24: string | null,
): ScatterplotLayer<FlightState>[] {
  const selected = selectedIcao24
    ? flights.find((f) => f.icao24 === selectedIcao24)
    : undefined;
  if (!selected) return [];
  return [
    new ScatterplotLayer<FlightState>({
      id: "selection-ring",
      data: [selected],
      getPosition: (d) => [d.lon, d.lat],
      getFillColor: [0, 0, 0, 0],
      stroked: true,
      filled: true,
      lineWidthUnits: "pixels",
      getLineWidth: 2,
      getLineColor: SELECTED_RING_COLOR,
      radiusUnits: "pixels",
      getRadius: 22,
      radiusMinPixels: 22,
      pickable: false,
    }),
  ];
}

// buildAircraftLayers composes the selection ring (if any) under the icon
// layer.
function buildAircraftLayers(
  flights: FlightState[],
  isReplay: boolean,
  selectedIcao24: string | null,
): Layer[] {
  return [
    ...(isReplay ? [] : buildSelectionRing(flights, selectedIcao24)),
    buildAircraftIconLayer(flights, isReplay, selectedIcao24),
  ];
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
  const mapRef = useRef<maplibregl.Map | null>(null);
  const replayAbortRef = useRef<AbortController | null>(null);

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
  const selectedIcao24 = useFlightStore((s) => s.selectedIcao24);
  const select = useFlightStore((s) => s.select);
  const clearSelection = useFlightStore((s) => s.clearSelection);

  const flightList = Object.values(flights);

  const isReplayActive = useReplayStore((s) => s.isActive);
  const replaySamples = useReplayStore((s) => s.samples);
  const replaySampleTimesMs = useReplayStore((s) => s.sampleTimesMs);
  const replayScrubMs = useReplayStore((s) => s.scrubMs);
  const startReplayLoading = useReplayStore((s) => s.startLoading);
  const loadReplayWindow = useReplayStore((s) => s.loadWindow);
  const setReplayError = useReplayStore((s) => s.setError);

  const replayFlights = useMemo(
    () =>
      computeFlightsAtTime(replaySamples, replaySampleTimesMs, replayScrubMs),
    [replaySamples, replaySampleTimesMs, replayScrubMs],
  );
  const displayedFlights = isReplayActive ? replayFlights : flightList;

  const handleStartReplay = () => {
    replayAbortRef.current?.abort();
    const abort = new AbortController();
    replayAbortRef.current = abort;

    const bounds = mapRef.current?.getBounds();
    const bbox = bounds ? clampBBox(bounds) : null;
    startReplayLoading();
    const to = new Date();
    const from = new Date(to.getTime() - REPLAY_WINDOW_MS);
    fetchReplayWindow(from, to, bbox ?? undefined, abort.signal)
      .then((samples) => {
        if (abort.signal.aborted) return;
        loadReplayWindow(samples, from.getTime(), to.getTime());
      })
      .catch((err: unknown) => {
        if (abort.signal.aborted) return;
        setReplayError(
          err instanceof Error ? err.message : "failed to load replay window",
        );
      });
  };

  // Exiting replay mode (or starting a fresh load) should discard any
  // in-flight fetchReplayWindow request, so a slow response from a
  // previous window can't land after the user has already left replay or
  // asked for a different one.
  useEffect(() => {
    if (!isReplayActive) {
      replayAbortRef.current?.abort();
      replayAbortRef.current = null;
    }
  }, [isReplayActive]);

  useEffect(() => {
    if (!containerRef.current) return;

    const map = new maplibregl.Map({
      container: containerRef.current,
      style: MAP_STYLE_URL,
      center: [0, 20],
      zoom: 2,
    });

    mapRef.current = map;

    const overlay = new MapboxOverlay({ interleaved: true, layers: [] });
    overlayRef.current = overlay;
    map.addControl(overlay as unknown as maplibregl.IControl);

    let socket: FlightSocket | null = null;
    let reloadAbort: AbortController | null = null;

    const reloadFromREST = () => {
      const bbox = clampBBox(map.getBounds());
      if (!bbox) return;
      reloadAbort?.abort();
      const abort = new AbortController();
      reloadAbort = abort;
      fetchFlightsByBBox(bbox, abort.signal)
        .then((data) => {
          if (abort.signal.aborted) return;
          setFlights(data);
        })
        .catch((err: unknown) => {
          if (abort.signal.aborted) return;
          setError(
            err instanceof Error ? err.message : "failed to reload flights",
          );
        });
    };

    const resubscribe = () => {
      const bbox = clampBBox(map.getBounds());
      if (!bbox) return;
      reloadAbort?.abort();

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
      reloadAbort?.abort();
      socket?.close();
      overlayRef.current = null;
      mapRef.current = null;
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
    overlayRef.current?.setProps({
      layers: buildAircraftLayers(
        displayedFlights,
        isReplayActive,
        selectedIcao24,
      ),
      // Clicking an aircraft selects it; clicking empty map clears the
      // selection. Replay mode is non-interactive for selection.
      onClick: (info: PickingInfo) => {
        if (isReplayActive) return;
        const picked = info.object as FlightState | undefined;
        if (picked?.icao24) {
          select(picked.icao24);
        } else {
          clearSelection();
        }
      },
    });
  }, [displayedFlights, isReplayActive, selectedIcao24, select, clearSelection]);

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
        <div>
          {displayedFlights.length} aircraft{" "}
          {isReplayActive ? "(replay)" : "in view"}
        </div>
        {!isReplayActive && lastUpdated && (
          <div className="text-xs">
            updated {lastUpdated.toLocaleTimeString()}
          </div>
        )}
        {!isReplayActive && error && (
          <div className="text-xs text-red-500">{error}</div>
        )}
      </div>
      {!isReplayActive && statusLabel && (
        <div className="absolute top-3 right-3 z-10 rounded bg-amber-900/90 px-3 py-2 text-left text-sm text-amber-100 shadow">
          {statusLabel}
        </div>
      )}
      {!isReplayActive && (
        <button
          type="button"
          onClick={handleStartReplay}
          className="absolute bottom-3 left-3 z-10 rounded bg-bg/90 px-3 py-2 text-sm text-text shadow"
        >
          replay last 30 min
        </button>
      )}
      {!isReplayActive && <FlightDetailDrawer />}
      <ReplayScrubber />
    </div>
  );
}
