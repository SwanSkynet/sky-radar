# Feature Batch 1: Coverage Fix, Flight Detail Panel, Aircraft Icons & Smoother Updates

## Status

Implemented. This was a self-contained batch built **first**, ahead of the
larger roadmap items (search/filters, watchlists, public-API depth) and ahead of
the deferred **aircraft-type classification** subsystem (see
[Out of scope](#out-of-scope-deferred)). It remains the design reference for the
shipped behaviour.

## Summary

The live site (https://skyradar.swanathiyarath.com/) currently shows only a
small cluster of aircraft around San Francisco, the dots don't carry any
detail-on-click, they're undifferentiated coloured circles, and they refresh in
~15s steps so they appear to teleport rather than move. This batch fixes the
coverage regression and lands three user-facing map improvements:

| # | Feature | One-line outcome |
|---|---------|------------------|
| 0 | **Ingest coverage & freshness fix** | Map shows aircraft beyond the SF bubble; the actual live-site bug. |
| 1 | **Click → flight detail panel** | Clicking an aircraft opens a drawer with its details. |
| 2 | **SVG aircraft icons + basic type classifier** | Aircraft render as heading-rotated, per-type SVGs instead of dots. |
| 3 | **Faster & smoother updates** | Lower update interval where rate limits allow + client-side interpolation so motion is smooth. |

> **Scope note:** Feature 2's per-type icons require knowing each aircraft's
> type, which the canonical model doesn't carry today. This batch therefore
> pulls in a **basic** aircraft-type classifier (capture the type code
> adsb.lol/airplanes.live already return + emitter category + military flag, map
> to the SVG buckets, generic fallback). The **full/fine-grained** type
> reference DB remains a later batch — see [Out of scope](#out-of-scope-deferred).

## Motivation / current state

- **Coverage:** [`cmd/adapter-adsblol/main.go`](../../cmd/adapter-adsblol/main.go)
  and [`cmd/adapter-airplaneslive/main.go`](../../cmd/adapter-airplaneslive/main.go)
  default to center `37.6188, -122.3758` (SFO) with a **250 NM** radius, and
  [`deploy/docker-compose.prod.yml`](../../deploy/docker-compose.prod.yml) sets
  no overriding `*_LAT` / `*_LON` / `*_RADIUS_NM` env vars. Both providers cap
  radius at 250 NM (`docs/api-docs/adsb-lol-api-docs.md`,
  `docs/api-docs/airplanes-live-docs.md`), so they are *inherently regional*.
  OpenSky is global (`/states/all`, no bbox set) but polls anonymously in prod
  (no `OPENSKY_CLIENT_ID` / `OPENSKY_CLIENT_SECRET`), so it is rate-limited to
  sporadic data. Net effect: the only continuous feed is the SF 250 NM bubble.
- **No selection:** the aircraft layer is `pickable: false`
  ([`web/src/map/MapView.tsx:67`](../../web/src/map/MapView.tsx#L67)); clicks do
  nothing. A `/flight-detail` drawer and "selected-flight" store slice are
  already *designed* in [`docs/tech-stack/frontend.md`](../tech-stack/frontend.md)
  but not built.
- **Dots, not icons:** rendered via `ScatterplotLayer`
  ([`web/src/map/MapView.tsx:52-69`](../../web/src/map/MapView.tsx#L52-L69)).
  `HeadingDeg` already exists on `FlightState` to drive icon rotation.
- **Step updates:** adapters poll every 15s and the normalizer merge loop runs
  every 15s (`defaultMergeInterval`), so positions arrive in 15s steps with no
  client-side interpolation — aircraft jump rather than glide.

## In scope

- Adapter coverage configuration + OpenSky credentialed global ingest.
- Make the live aircraft layer pickable and add a selected-flight detail drawer.
- Replace the live `ScatterplotLayer` with a heading-rotated `IconLayer` backed
  by user-supplied SVGs, with a graceful fallback.
- Tune ingest/merge cadence within provider rate limits and add client-side
  dead-reckoning interpolation between updates.

## Out of scope (deferred)

- **Full / fine-grained aircraft-type classification.** A *basic* classifier
  ships in this batch (Feature 2) using the type code + emitter category +
  military flag the providers already return. Deferred: a comprehensive ICAO
  type-designator reference DB, military hex-range tables, and callsign/operator
  heuristics that would let us reliably distinguish the niche military SVGs
  (awacs / stealth / reconnaissance / maritime_patrol / tanker) from a generic
  military icon for *every* aircraft. Those buckets are wired now but only
  populate where the seed type table or provider data is confident.
- **OpenSky-sourced aircraft carry no type** (the `/states/all` feed has no type
  field), so globally they fall back to the generic icon; type-specific icons
  appear where adsb.lol/airplanes.live data is available.
- Search / filters, watchlists, geofences, replay changes — unchanged this batch.

---

## Feature 0 — Ingest coverage & freshness fix

**Goal:** the map shows aircraft well beyond San Francisco, continuously, and a
zoomed-out (global) viewport shows worldwide aircraft.

**Mental model — ingestion is decoupled from the viewport.** The adapters are
background pollers that continuously fill Redis regardless of who is looking; the
frontend then queries Redis *by viewport bbox* (already working — see the
GEORADIUS / full-scan logic in
[`internal/redisutil/flightstate.go`](../../internal/redisutil/flightstate.go)).
A viewport can only render what has already been ingested. So "full globe →
global coverage" is **not** something the map view triggers on demand — it
requires a **global ingestion source running continuously**, which is OpenSky
`/states/all` with credentials. Once OpenSky runs credentialed and global, any
viewport (including the whole world) automatically returns worldwide aircraft.

**Reality of the providers:**
- adsb.lol & airplanes.live are **capped at 250 NM** per request — widening a
  single center is not possible. They are also **shared, server-side** pollers
  serving every visitor at once, so they cannot follow any one user's camera or
  geolocation. Options: (a) pin them at a fixed high-traffic region, (b) accept
  them as regional supplements, or (c) add a multi-point poll grid (heavier; a
  follow-up, not this batch). They are the **only source of aircraft type**, so
  per-type icons are richest inside whatever region they're pinned to.
- **OpenSky `/states/all` is the global source.** The real fix for "show the
  world" is configuring OpenSky OAuth2 credentials so it is no longer throttled
  to anonymous limits. OpenSky carries **no aircraft type**, so globally-sourced
  aircraft fall back to the default icon.

**Per-user experience vs. shared ingestion (clarifying a common misconception):**
- *What's displayed* already follows the camera: the frontend sends its viewport
  bbox to the API, which returns only in-bbox aircraft. Zooming to the whole
  globe queries the whole world and returns whatever global data is in Redis — so
  "full globe ⇒ global data" holds **once OpenSky global ingestion is on**.
- *What's ingested* does **not** follow the camera. Adapters are shared
  background pollers; there is no single "current location" for the backend. Each
  user's camera merely queries the shared Redis pool.

**Frontend — initial map center from geolocation:** open the map centered on the
visitor's browser geolocation (`navigator.geolocation`) instead of the current
fixed `center: [0, 20], zoom: 2` ([`MapView.tsx:164`](../../web/src/map/MapView.tsx#L164)).
This is a per-user *view* default only — it does not change what is ingested.
Fall back to the current world view if permission is denied/unavailable, and
don't block first paint on the permission prompt.

**Changes:**
1. Add OpenSky credentials to the prod environment (`OPENSKY_CLIENT_ID`,
   `OPENSKY_CLIENT_SECRET`) — already wired in
   [`deploy/docker-compose.prod.yml`](../../deploy/docker-compose.prod.yml),
   just unset. **Credentials provided** (client id
   `swanskynet@gmail.com-api-client`); the secret goes only in the droplet's
   prod `.env` + GitHub Actions secrets — **never committed to the repo**. Run
   OpenSky **fully global** (no `OPENSKY_LAMIN/...` bbox) and at
   `POLL_INTERVAL_SECONDS=15` (credentialed — conservative on the credit budget;
   see Feature 3).
2. **Pin the two regional adapters to Southern California** (decided):
   `*_LAT=34.05`, `*_LON=-118.24`, `*_RADIUS_NM=250`. Rationale: dense commercial
   traffic (LAX/SAN/LAS) plus heavy military variety (Edwards, China Lake,
   Nellis, Naval bases) within 250 NM, so the per-type/military SVGs are actually
   exercised. This is a config choice, trivially changed via env later.

**Env reference (current names & defaults):**

| Adapter | Lat | Lon | Radius | Poll interval |
|---------|-----|-----|--------|---------------|
| adsb.lol | `ADSBLOL_LAT` (37.6188) | `ADSBLOL_LON` (-122.3758) | `ADSBLOL_RADIUS_NM` (250, max 250) | `POLL_INTERVAL_SECONDS` (15s) |
| airplanes.live | `AIRPLANES_LIVE_LAT` (37.6188) | `AIRPLANES_LIVE_LON` (-122.3758) | `AIRPLANES_LIVE_RADIUS_NM` (250, max 250) | `POLL_INTERVAL_SECONDS` (15s) |
| OpenSky | `OPENSKY_LAMIN/LOMIN/LAMAX/LOMAX` (unset = global) | — | — | `POLL_INTERVAL_SECONDS` (30s) |

**Acceptance:** with prod config applied, a continental/zoomed-out viewport
returns aircraft from more than just the SF region; OpenSky poll error/throttle
rate drops (visible via `skyradar.adapter.poll.errors` /
`skyradar.adapter.source.freshness`).

---

## Feature 1 — Click → flight detail panel

**Goal:** clicking an aircraft selects it and opens a detail drawer.

**Changes (frontend only):**
1. Set `pickable: true` on the live aircraft layer and add an `onClick` that
   writes the picked `FlightState.icao24` into the store.
2. Add a `selectedIcao24: string | null` slice + `select(icao24)` /
   `clearSelection()` to [`web/src/store/useFlightStore.ts`](../../web/src/store/useFlightStore.ts).
   Keep the selection by **icao24**, not by object reference, so the drawer keeps
   showing fresh data as new WS messages update that aircraft.
3. New `FlightDetailDrawer` component (per `frontend.md`'s `/flight-detail`),
   subscribed to `flights[selectedIcao24]`, rendering the field set below.
   Highlight the selected aircraft on the map (e.g. ring / larger icon). Close on
   drawer-close, Esc, or clicking empty map.

**Field set (decided)** — built only from fields available after Feature 2's
backend lands; omit/grey rows whose value is null:

| Group | Rows |
|-------|------|
| Header | Callsign (fallback ICAO24); **military** badge if flagged; **stale** badge if stale |
| Identity | ICAO24 hex · Registration · Aircraft type (ICAO designator + classified bucket) |
| Altitude | Barometric ft (primary) · Geometric ft (secondary) |
| Motion | Ground speed kt · Vertical rate fpm (climb/descend arrow) · Heading ° |
| Status | On-ground · Squawk (+ emergency decode 7500 hijack / 7600 radio-fail / 7700 emergency) |
| Position | Lat/Lon · Position quality (ADS-B / MLAT / estimated) |
| Provenance | Sources list · Last seen ("N s ago" from `last_seen_utc`) |

Type/military rows come from Feature 2's new fields; until that lands they read
"unknown".

**Acceptance:** clicking an aircraft opens the drawer with its current fields;
the drawer updates live as new messages arrive for that aircraft; the selected
aircraft is visually distinguished; closing clears selection.

---

## Feature 2 — SVG aircraft icons + basic type classifier

**Goal:** aircraft render as per-type SVGs rotated to their heading instead of
dots. Because the SVG set is type-specific, this requires classifying each
aircraft's type — so the feature spans backend (capture + classify) and frontend
(render).

**Provided assets** ([`web/src/assets/`](../../web/src/assets/)): one SVG per
type bucket —
`commercial_jet`, `widebody`, `business_jet`, `cargo`, `turboprop`,
`utility_helicopter`, `attack_helicopter`,
`military_transport`, `military_drone`, `tanker`, `awacs`, `reconnaissance`,
`maritime_patrol`, `stealth`.
There is no dedicated generic/unknown SVG in the set; **unclassified and
OpenSky-only aircraft default to `commercial_jet.svg`** (decided).

### Backend — capture + classify

1. **Adapter parse:** adsb.lol & airplanes.live raw responses already include the
   ICAO type designator `t` (e.g. `"A320"`), registration `r`, and in full
   responses an emitter `category` (A1–A7…) and `dbFlags` (military bit). The
   adapters currently drop these — capture `t`, `category`, and a `military`
   flag. (OpenSky `/states/all` has none of these → those aircraft stay
   unclassified → generic icon.)
2. **Model:** add nullable fields to
   [`FlightState`](../../internal/flightmodel/flightstate.go) — e.g.
   `AircraftType *string` (raw ICAO designator), `EmitterCategory *string`,
   `Military bool`, and a derived `IconClass *string` (one of the buckets
   above). Keep all nullable/back-compatible; update the Redis encode/decode and
   the JSON wire shape used by the API/WS.
3. **Merge:** carry these through `MergeAll` with the same provider-precedence
   rules as other fields; a provider that supplies `t` wins over one that
   doesn't.
4. **Classifier (`internal/...` — seed table):** map to an `IconClass` bucket via,
   in priority order: (a) `military` flag + `t` → specific military bucket
   (e.g. `E3*`→awacs, `KC*`→tanker, `RQ4/MQ*`→military_drone, `C17/C130`→
   military_transport, `P8/P3`→maritime_patrol, `U2/RC*`→reconnaissance,
   `F22/F35/B2`→stealth) else generic military; (b) emitter category
   `A7`→helicopter, `A5`→widebody; (c) `t` → civil bucket via a seed
   designator→class table (`A3xx/B73x/B77x`→commercial_jet or widebody,
   bizjet families→business_jet, `B74F/B77F` freighters→cargo, turboprops→
   turboprop); (d) fallback → generic/default. The seed table is intentionally
   small and grows later (the deferred full DB).

### Frontend — render

5. Replace the live-mode `ScatterplotLayer` with deck.gl `IconLayer` (the
   documented intended layer in `frontend.md`); pick the SVG by `IconClass`,
   defaulting unknown to the generic/default icon. Keep replay mode's distinct
   amber treatment.
6. `getAngle` from `HeadingDeg` (null heading → north / a no-heading variant).
   Convert aviation heading (CW-from-north) to deck.gl's angle convention.
7. Build an icon atlas from the SVGs (rasterized per device-pixel-ratio / zoom
   sizing) so `IconLayer` stays performant with data-prop diffing at
   viewport-bounded counts.
8. Preserve the staleness signal under icons: stale → grey/desaturated tint or
   reduced opacity (carry over today's blue-fresh / grey-stale semantics from
   [`MapView.tsx:29-30`](../../web/src/map/MapView.tsx#L29-L30)).
9. Surface type + military flag in the Feature 1 detail drawer too (a "type" row
   that's now populated instead of a placeholder).

**Acceptance:** aircraft with known type render as the matching SVG rotated to
heading; unknown/OpenSky-only aircraft render the generic icon; military
aircraft route to a military icon; stale aircraft stay visually distinct;
selection highlight still works; pan/zoom stays smooth at expected counts.

---

## Feature 3 — Faster & smoother updates

**Goal:** updates feel continuous, not 15s teleports.

Two independent levers:

1. **Interval tuning (backend, bounded by rate limits) — decided values:**
   - Regional adapters (adsb.lol, airplanes.live): `POLL_INTERVAL_SECONDS=10`.
   - OpenSky: `POLL_INTERVAL_SECONDS=15` (credentialed; conservative on the
     credit budget — do not go lower without watching credit/error metrics).
   - Normalizer: `MERGE_INTERVAL_SECONDS=10`.
   - Constraint: do **not** lower blindly; watch `skyradar.adapter.poll.errors`
     and stay within the master-PRD freshness SLO (P95 ≤ 15s). These are env
     changes only — no code change, so they're cheap to revert.

2. **Client-side interpolation (frontend — the bigger perceived win, no extra
   network cost).**
   - Between server updates, advance each aircraft's position by dead-reckoning
     from its last known `lat/lon`, `ground_speed_kt`, and `heading_deg` on a
     `requestAnimationFrame` / deck.gl transition loop. Snap to the authoritative
     position when the next real update arrives.
   - Guard: don't interpolate `on_ground` aircraft or ones already `stale`; cap
     extrapolation age so a lost aircraft doesn't fly off indefinitely.

**Acceptance:** aircraft visibly move between server updates rather than jumping;
positions reconcile to server truth on each update without visible snapping
artifacts for normal cases; no increase in dropped/late-poll metrics from any
interval change.

---

## Functional requirements

| ID | Requirement | Acceptance criteria |
|----|-------------|---------------------|
| FB1-FR1 | Zoomed-out / continental viewport returns aircraft beyond the SF region | Manual check on prod after config; regional vs global viewport both non-empty |
| FB1-FR2 | OpenSky runs credentialed in prod with reduced throttling | `OPENSKY_CLIENT_ID/SECRET` set; poll-error/freshness metrics improve |
| FB1-FR2a | Map opens centered on the visitor's geolocation, with graceful fallback | Manual check (allow + deny permission); unit test on the fallback path |
| FB1-FR3 | Clicking an aircraft opens a detail drawer for that aircraft | Manual + component test: click sets `selectedIcao24`, drawer renders its fields |
| FB1-FR4 | Detail drawer reflects live updates for the selected aircraft | Test: new WS message for selected icao24 updates drawer without reselect |
| FB1-FR5 | Selection is clearable (close / Esc / empty-map click) | Manual + unit test on store `clearSelection` |
| FB1-FR6 | Live aircraft render as per-type SVG icons rotated to heading | Visual check; unit test on heading→angle conversion incl. null heading |
| FB1-FR6a | Adapters capture aircraft type / category / military flag; fields flow through model → Redis → API/WS | Adapter parse tests against fixtures; round-trip encode/decode test |
| FB1-FR6b | Classifier maps type/category/military to the correct SVG bucket with generic fallback | Table-driven unit test covering each bucket + unknown |
| FB1-FR7 | Stale aircraft remain visually distinct under the icon layer | Visual check; stale styling test |
| FB1-FR8 | Aircraft positions interpolate smoothly between server updates | Visual check; interpolation reconciles to server position on update |
| FB1-FR9 | No regression in poll/merge reliability from cadence changes | `skyradar.adapter.poll.errors` flat or lower after change; merge cycle within interval |

## Implementation sequencing

1. **Feature 0 config** (fastest, fixes the live bug) — env only, no code unless
   we add a poll grid. Ship and verify coverage first.
2. **Feature 2 backend** (capture + model + merge + classifier) — independent of
   the frontend; lands the new fields on the wire so the UI can consume them.
3. **Feature 1 (detail panel)** — store slice → `pickable`/`onClick` → drawer;
   can show the new type/military fields from step 2.
4. **Feature 2 frontend (icons)** — `IconLayer` + atlas + class→SVG mapping;
   reuses the selection highlight from Feature 1.
5. **Feature 3 (cadence + interpolation)** — interpolation last (depends on the
   layer choice from Feature 2); interval tuning can land alongside Feature 0.

## Testing

- Go: `go test ./...` (adapter config / any backend cadence change).
- Frontend: `web/package.json` scripts — unit tests for the store selection
  slice, heading→angle conversion, interpolation reconciliation; component test
  for the drawer. Lint: `golangci-lint run ./...`.
- Manual prod verification of coverage (FB1-FR1/2) after config rollout.

## Inputs needed from you

1. ~~**SVG assets**~~ — ✅ provided in [`web/src/assets/`](../../web/src/assets/)
   (14 type-specific SVGs). Unknown/OpenSky aircraft default to
   `commercial_jet.svg` (decided).
2. ~~**OpenSky credentials**~~ — ✅ provided (set in droplet `.env` + GH secrets,
   not committed). Run OpenSky **fully global** (no bbox) for worldwide coverage.
3. ~~**Initial map center**~~ — ✅ from browser geolocation, world-view fallback
   (decided).
4. ~~**Regional-adapter pin**~~ — ✅ Southern California (`34.05, -118.24`,
   250 NM). Easily overridable via env.
5. ~~**Detail-panel field set**~~ — ✅ see the field-set table in Feature 1.
6. ~~**Cadence targets**~~ — ✅ regional poll 10s, OpenSky 15s, merge 10s, plus
   client-side interpolation (Feature 3).

All inputs resolved — this batch is ready to implement via the prompts in
[`prompts/phase-5/`](../../prompts/phase-5/).
