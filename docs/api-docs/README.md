# Data Provider Comparison

Sky Radar's v1 source adapters (see [`../tech-stack/backend.md`](../tech-stack/backend.md)) wrap these three providers. This page exists so adapter implementers and reviewers can see provider differences at a glance before reading each provider's full doc.

| Provider | Auth required | Rate limit | Coverage type | Notes |
|---|---|---|---|---|
| [OpenSky Network](opensky-api-docs.md) | Yes (OAuth2 client credentials) for protected endpoints; `GET /states/all` works without auth but is rate limited | `GET /states/all` is rate limited for non-owner queries; `GET /states/own` is unlimited with your own sensors | Global, research/open-use network | Most structured/official-feeling API of the three; flight-history endpoints are batch/nightly, not real-time — only `/states/all` and `/states/own` matter for live tracking |
| [adsb.lol](adsb-lol-api-docs.md) | No (today) | Not formally documented; provider asks for coordination before production use | Community-fed, global | ODbL v1.0 licensed; provider has signaled API keys may become required in the future — adapter must be built so adding auth later is a config change, not a rewrite |
| [airplanes.live](airplanes-live-docs.md) | No | Not formally documented; explicitly "non-commercial use" | Community-fed, global, includes ADS-B/MLAT/TIS-B/ADS-C/Mode-S | Richest per-aircraft field set (nav modes, surveillance quality, weather estimates); "non-commercial" framing is consistent with Sky Radar's no-revenue model but should be re-confirmed against current ToS before any future commercial-adjacent use |

## Why all three, not just one
No single source guarantees global, continuous coverage (see [the master PRD's background section](../prd/00-master-prd.md#3-background-and-problem-statement)). Each adapter is independent (see [ADR-0001](../decisions/0001-backend-language-go.md) and [`system-architecture.md`](../architecture/system-architecture.md)), so this list can grow or shrink without touching normalization, event detection, or the API.

## Field mapping starting points (provider → canonical `FlightState`)
Full canonical schema: [`../architecture/data-model.md`](../architecture/data-model.md).

| Canonical field | OpenSky | adsb.lol | airplanes.live |
|---|---|---|---|
| `icao24` | `icao24` (state vector index 0) | path param `icao_hex` / response field | `hex` |
| `callsign` | `callsign` (index 1) | response field | `flight` |
| `lat` / `lon` | `latitude`/`longitude` (indices 6/5) | response fields | `lat`/`lon` |
| `altitude_baro_ft` | `baro_altitude` (index 7, meters → convert to ft) | response field | `alt_baro` (may be the string `"ground"` — handle explicitly) |
| `ground_speed_kt` | `velocity` (index 9, m/s → convert to kt) | response field | `gs` |
| `heading_deg` | `true_track` (index 10) | response field | `track` |
| `on_ground` | `on_ground` (index 8) | response field | derived: `alt_baro == "ground"` |
| `position_quality` | derive from `position_source` (index 16: 0=ADS-B, 2=MLAT → map accordingly) | not explicitly provided; default `adsb` unless otherwise indicated | derive from `type` field (`adsb_icao` → adsb, `mlat` → mlat, etc.) |

This table is a starting point for whoever implements each adapter, not a substitute for reading the full provider doc — unit conversions and null-handling per field still need to be done carefully (e.g., OpenSky's altitude/speed are metric; airplanes.live's `alt_baro` is polymorphic).

## Licensing reminder
Per [the master PRD](../prd/00-master-prd.md#5-data-sources-and-licensing), every served `FlightState` carries a `sources` field crediting which of these providers contributed to it, and the product includes a public attribution page. Before adding a fourth provider, check its terms against the same bar these three were evaluated against (free, no-payment, redistribution-compatible with an open-source, no-revenue project).
