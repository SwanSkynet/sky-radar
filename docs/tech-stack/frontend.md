# Frontend Stack

See [ADR-0004](../decisions/0004-map-rendering-maplibre-deckgl.md) for the map rendering rationale.

## Core stack
| Concern | Choice | Why |
|---|---|---|
| Framework | React + TypeScript | Widely known, strong typing against the API schema, large ecosystem for map/UI components |
| Build tool | Vite | Fast dev server and HMR, simple config, fast CI builds |
| Base map | MapLibre GL JS | Free/open-source WebGL vector-tile renderer, using CartoDB Dark Matter style for a professional, high-contrast dark-theme |
| Aircraft layer | deck.gl (`ScatterplotLayer`/`IconLayer`) | Built for tens of thousands of frequently-updating points, layers directly on MapLibre |
| State management | Zustand | Minimal boilerplate for map viewport, selected-flight, filter, and live-data state; avoids Redux ceremony for a project this size |
| Styling | Tailwind CSS | Fast to build consistent UI without a custom design system overhead |
| Data fetching (REST/GraphQL) | `graphql-request` + native `fetch` | Lightweight; no need for a heavier client (Apollo/urql) given query patterns are simple and mostly read-only |
| Live data | Native WebSocket API wrapped in a small reconnect/backoff hook | Matches the gateway's WebSocket contract (see [`backend.md`](backend.md)) without an external real-time library |

## Repo layout
```
/web
  /src
    /map            # MapLibre + deck.gl integration, viewport state, layer config
    /flight-detail  # detail drawer component
    /search-filter  # search bar + filter controls
    /events         # event feed component
    /replay         # time replay scrubber + playback state
    /metrics        # live engineering metrics panel
    /status         # public architecture/status page
    /api            # typed REST/GraphQL/WebSocket client wrappers
    /store          # Zustand stores
  vite.config.ts
  package.json
```

## Performance budget enforcement
- Lighthouse CI runs on every PR against a staging build, gating the ≤150ms map-interaction and ≤2.5s initial-load targets defined in the [master PRD](../prd/00-master-prd.md#8-non-functional-requirements-slos).
- The aircraft layer's data is updated via deck.gl's data prop diffing (not full re-renders), keeping pan/zoom smooth independent of how many aircraft are currently tracked globally — the viewport-scoped WebSocket subscription (see [`backend.md`](backend.md)) bounds how many aircraft the client ever has to render at once.
- Bundle size is tracked in CI; MapLibre + deck.gl are the two heaviest dependencies and are loaded eagerly (the map is the landing experience), everything else (replay, metrics panel, status page) is code-split and lazy-loaded.

## Degraded-mode UI
Per [FR-12 in the master PRD](../prd/00-master-prd.md), the frontend must visually distinguish live/fresh data from stale/degraded data. This is implemented as:
- A per-aircraft staleness indicator driven by the `stale` field on `FlightState` (see [`../architecture/data-model.md`](../architecture/data-model.md)).
- A global degraded-mode banner driven by the `/status` endpoint's aggregate freshness metric, shown whenever P95 freshness crosses the documented threshold.
