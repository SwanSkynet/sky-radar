# ADR-0004: Map rendering is MapLibre GL JS + deck.gl

## Status
Accepted

## Context
The frontend must render up to tens of thousands of moving aircraft markers on a world map with smooth pan/zoom/filter interactions (≤150ms target, see [`00-master-prd.md`](../prd/00-master-prd.md#8-non-functional-requirements-slos)), entirely with free/open-source tooling. Candidates considered: Leaflet, MapLibre GL JS alone, MapLibre GL JS + deck.gl.

## Decision
The base map is MapLibre GL JS; the aircraft layer is rendered with a deck.gl WebGL overlay on top of it.

## Rationale
- **Rendering technology must match the data volume.** Leaflet renders markers as DOM/SVG/Canvas elements per-marker; at the target scale this degrades well before the 150ms interaction budget is met. WebGL-based rendering (MapLibre's vector tiles, deck.gl's instanced point layers) is the only realistic way to hit that budget at tens of thousands of points.
- **MapLibre is the free, open-source path.** It's a community-maintained fork of Mapbox GL JS from the point Mapbox moved to a proprietary license — same rendering quality and API shape, no licensing cost, consistent with the no-revenue model. To align with professional airspace intelligence platform visuals, the CartoDB Dark Matter tile stylesheet is used to provide a high-contrast dark theme out-of-the-box without requiring proprietary API keys (e.g. Mapbox or Google Maps).
- **deck.gl solves the part MapLibre doesn't.** MapLibre's own marker/symbol layers are not built for tens of thousands of independently updating points at high frequency; deck.gl's `ScatterplotLayer`/`IconLayer` are purpose-built for exactly this (large, frequently-updating point datasets), and deck.gl is designed to layer directly on top of a MapLibre base map rather than replace it.
- **This combination is a real, current best practice** for this category of problem (live large-scale geospatial visualization), not a novelty choice — it's directly transferable knowledge for anyone reading or contributing to the project.

## Rejected alternatives
- **MapLibre GL JS alone** — viable at lower density with custom clustering, but pushes the project into building bespoke high-density marker clustering/LOD logic that deck.gl already solves; revisit only if deck.gl proves to be unnecessary overhead in practice.
- **Leaflet** — simplest and most widely known option, but canvas/SVG rendering is the wrong tool for this data volume; would require working against the library's grain to hit the latency target.

## Consequences
- Frontend stack is React + TypeScript + MapLibre GL JS + deck.gl, detailed in [`frontend.md`](../tech-stack/frontend.md).
- Aircraft position updates from the WebSocket subscription feed deck.gl's layer data directly; viewport changes drive both the MapLibre camera and the WebSocket subscription's bounding box (see [`system-architecture.md`](../architecture/system-architecture.md)).
- Performance budgets are enforced in CI via Lighthouse/perf-budget checks against this stack specifically (see [phase-2-realtime-systems.md](../prd/phase-2-realtime-systems.md)).
