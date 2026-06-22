# adsb.lol API Documentation

## Overview

- Base URL: `https://api.adsb.lol`
- OpenAPI spec: `https://api.adsb.lol/api/openapi.json`
- Version: `0.0.2`
- License: ODbL v1.0
- Usage: API is free and open source. Production use should be coordinated with the provider to avoid accidental breaking changes.
- Notes: The docs page also notes that feeders can gain access to direct `readsb` re-api and raw aggregated data.

## General usage

- Most endpoints are `GET` requests.
- Responses are returned in JSON.
- No authentication is currently required for the public endpoints, but the provider may require an API key in the future.

## /v2 endpoints

### GET /v2/pia
- Returns aircraft with PIA addresses (Privacy ICAO Address).

### GET /v2/mil
- Returns military registered aircraft.

### GET /v2/ladd
- Returns aircraft on LADD (Limiting Aircraft Data Displayed).

### GET /v2/sqk/{squawk}
- Returns aircraft filtered by transponder squawk code.
- Path parameter:
  - `squawk` (string) — example: `1200`, `7700`

### GET /v2/squawk/{squawk}
- Same as `/v2/sqk/{squawk}`.
- Path parameter:
  - `squawk` (string)

### GET /v2/type/{aircraft_type}
- Returns aircraft filtered by aircraft type designator.
- Path parameter:
  - `aircraft_type` (string) — example: `A320`, `B738`

### GET /v2/reg/{registration}
- Returns aircraft filtered by registration.
- Path parameter:
  - `registration` (string) — example: `G-KELS`

### GET /v2/registration/{registration}
- Same as `/v2/reg/{registration}`.
- Path parameter:
  - `registration` (string)

### GET /v2/icao/{icao_hex}
- Returns aircraft filtered by ICAO hex code.
- Path parameter:
  - `icao_hex` (string) — example: `4CA87C`

### GET /v2/hex/{icao_hex}
- Same as `/v2/icao/{icao_hex}`.
- Path parameter:
  - `icao_hex` (string)

### GET /v2/callsign/{callsign}
- Returns aircraft filtered by callsign.
- Path parameter:
  - `callsign` (string) — example: `JBU1942`

### GET /v2/lat/{lat}/lon/{lon}/dist/{radius}
- Returns aircraft within a circular area around a point.
- Path parameters:
  - `lat` (number) — latitude of center point
  - `lon` (number) — longitude of center point
  - `radius` (integer) — radius in nautical miles, up to 250

### GET /v2/point/{lat}/{lon}/{radius}
- Same as `/v2/lat/{lat}/lon/{lon}/dist/{radius}`.
- Path parameters:
  - `lat` (number)
  - `lon` (number)
  - `radius` (integer)

### GET /v2/closest/{lat}/{lon}/{radius}
- Returns the single aircraft closest to a point inside a radius.
- Path parameters:
  - `lat` (number)
  - `lon` (number)
  - `radius` (integer)

## Legacy / v0 endpoints

### GET /api/0/airport/{icao}
- Returns airport data by ICAO code.
- Path parameter:
  - `icao` (string)

### POST /api/0/routeset
- API route set endpoint.
- Request body details are not documented in the public schema.

### GET /0/me
- Returns information about your receiver and global stats.

### GET /0/my
- Redirects to a map URL based on the caller's IP.

## Example requests

```bash
curl 'https://api.adsb.lol/v2/icao/4CA87C'
```

```bash
curl 'https://api.adsb.lol/v2/lat/51.4700/lon/-0.4543/dist/50'
```

## Additional notes

- The public docs page indicates future API key requirements for production usage.
- The API is explicitly free today, but provider coordination is recommended before building production integrations.
- The `v2` endpoints are the main aircraft-query interface; the legacy `/api/0` and `/0` endpoints provide receiver/map metadata.
