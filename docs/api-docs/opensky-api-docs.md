# OpenSky API Documentation

## Overview

- Base URL: `https://opensky-network.org/api`
- Documentation: https://openskynetwork.github.io/opensky-api/rest.html
- API version: OpenSky Network API 1.4.0
- Authentication: required for `GET /states/own` and most flight/track endpoints.
- Rate limits: `GET /states/all` is rate limited; `GET /states/own` is not rate limited when using your own sensors.

## Authentication

OpenSky uses OAuth2 client credentials for protected endpoints.

Example token request:

```bash
export CLIENT_ID=your_client_id
export CLIENT_SECRET=your_client_secret

token=$(curl -s -X POST "https://auth.opensky-network.org/auth/realms/opensky-network/protocol/openid-connect/token" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=client_credentials" \
  -d "client_id=$CLIENT_ID" \
  -d "client_secret=$CLIENT_SECRET" | jq -r '.access_token')
```

Use the token in requests:

```bash
curl -H "Authorization: Bearer $token" https://opensky-network.org/api/states/own
```

## Endpoints

### GET /states/all

Retrieve state vectors for the entire OpenSky network.

Request parameters:
- `time` (integer, optional) — Unix timestamp in seconds. Defaults to current time.
- `icao24` (string, optional) — one or more ICAO24 addresses as hex strings (e.g. `abc9f3`). Repeat parameter for multiple values.
- `lamin` (float, optional) — lower bound latitude in decimal degrees.
- `lomin` (float, optional) — lower bound longitude in decimal degrees.
- `lamax` (float, optional) — upper bound latitude in decimal degrees.
- `lomax` (float, optional) — upper bound longitude in decimal degrees.
- `extended` (integer, optional) — set to `1` to request aircraft category information.

Notes:
- Bounding-box queries require all four parameters: `lamin`, `lomin`, `lamax`, `lomax`.
- This endpoint is rate limited for non-owner queries.

Example:

```bash
curl "https://opensky-network.org/api/states/all?time=1458564121&icao24=3c6444"
```

```bash
curl "https://opensky-network.org/api/states/all?lamin=45.8389&lomin=5.9962&lamax=47.8229&lomax=10.5226"
```

Response:

```json
{
  "time": 1458564121,
  "states": [
    [
      "3c6444",
      "DLH123 ",
      "Germany",
      1458564118,
      1458564118,
      9.9935,
      53.5553,
      11300.8,
      false,
      222.5,
      40.7,
      0,
      [12345],
      11100.0,
      "7000",
      false,
      0,
      2
    ]
  ]
}
```

State vector fields:
- `icao24` (string)
- `callsign` (string|null)
- `origin_country` (string)
- `time_position` (int|null)
- `last_contact` (int)
- `longitude` (float|null)
- `latitude` (float|null)
- `baro_altitude` (float|null)
- `on_ground` (boolean)
- `velocity` (float|null)
- `true_track` (float|null)
- `vertical_rate` (float|null)
- `sensors` (int[]|null)
- `geo_altitude` (float|null)
- `squawk` (string|null)
- `spi` (boolean)
- `position_source` (int)
  - `0` = ADS-B
  - `1` = ASTERIX
  - `2` = MLAT
  - `3` = FLARM
- `category` (int)
  - `0` = No information at all
  - `1` = No ADS-B Emitter Category Information
  - `2` = Light (< 15500 lbs)
  - `3` = Small (15500 to 75000 lbs)
  - `4` = Large (75000 to 300000 lbs)
  - `5` = High Vortex Large
  - `6` = Heavy (> 300000 lbs)
  - `7` = High Performance

### GET /states/own

Retrieve state vectors from your own sensors without rate limitations.
Authentication is required.

Request parameters:
- `time` (integer, optional) — Unix timestamp in seconds. Defaults to current time.
- `icao24` (string, optional) — one or more ICAO24 addresses as hex strings.
- `serials` (integer, optional) — sensor serial number. Repeat to request multiple receivers.

Response:
- Same JSON structure as `/states/all`.

Example:

```bash
curl -H "Authorization: Bearer $token" -s "https://opensky-network.org/api/states/own?serials=123456"
```

### GET /flights/all

Retrieve flights for a time interval.

Required parameters:
- `begin` (integer) — start of interval as Unix time.
- `end` (integer) — end of interval as Unix time.

Notes:
- The time interval must not be larger than two hours.
- Returns HTTP 404 if no flights are found.

Example:

```bash
curl -H "Authorization: Bearer $token" -s "https://opensky-network.org/api/flights/all?begin=1517227200&end=1517230800"
```

### GET /flights/aircraft

Retrieve flights for a specific aircraft within an interval.

Required parameters:
- `icao24` (string) — lower-case ICAO24 address.
- `begin` (integer) — start of interval as Unix time.
- `end` (integer) — end of interval as Unix time.

Notes:
- The time interval must not be larger than two days.
- Flight data is updated by a nightly batch process, so only previous-day or older flights are generally available.

Example:

```bash
curl -H "Authorization: Bearer $token" -s "https://opensky-network.org/api/flights/aircraft?icao24=3c675a&begin=1517184000&end=1517270400"
```

### GET /flights/arrival

Retrieve arrivals for an airport within an interval.

Required parameters:
- `airport` (string) — ICAO airport code.
- `begin` (integer) — start of interval as Unix time.
- `end` (integer) — end of interval as Unix time.

Notes:
- Arrival data is updated nightly; only previous-day or earlier arrivals are likely available.

Example:

```bash
curl -H "Authorization: Bearer $token" -s "https://opensky-network.org/api/flights/arrival?airport=EDDF&begin=1517227200&end=1517230800"
```

### GET /flights/departure

Retrieve departures for an airport within an interval.

Required parameters:
- `airport` (string) — ICAO airport code.
- `begin` (integer) — start of interval as Unix time.
- `end` (integer) — end of interval as Unix time.

Notes:
- The time interval must cover more than two days (UTC).
- Returns HTTP 404 if no flights are found.

Example:

```bash
curl -H "Authorization: Bearer $token" -s "https://opensky-network.org/api/flights/departure?airport=EDDF&begin=1517227200&end=1517230800"
```

### GET /tracks

Experimental aircraft track endpoint.

Request parameters:
- `icao24` (string) — lower-case ICAO24 address.
- `time` (integer) — Unix time in seconds. Use `0` to request the live track for an ongoing flight.

Response:
- JSON object with track metadata and a sequence of waypoints.

Track waypoint fields:
- `time` (integer)
- `latitude` (float|null)
- `longitude` (float|null)
- `baro_altitude` (float|null)
- `true_track` (float|null)
- `on_ground` (boolean)

Example:

```bash
curl -H "Authorization: Bearer $token" -s "https://opensky-network.org/api/tracks?icao24=3c4b26&time=0"
```

## Notes

- The OpenSky endpoint `/states/all` is suitable for global network queries but may be subject to rate limiting.
- Use `/states/own` for data from your own sensors and to avoid rate limits.
- The flight endpoints are primarily historical and typically return data from the previous day or earlier.
- The `/tracks` endpoint is experimental and may be unstable.
