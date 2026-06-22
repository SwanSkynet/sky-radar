# Airplanes.live API Reference

## Overview

The Airplanes.live REST API provides real-time aircraft tracking data derived from ADS-B, MLAT, TIS-B, ADS-C, and Mode-S sources.

**Base URL**

```
https://api.airplanes.live/v2
```

### Notes

* No authentication is currently required.
* Intended for non-commercial use.
* Aircraft data is near real-time.
* Fields may be omitted when data is unavailable.
* All timestamps are Unix Epoch timestamps.

---

# Response Structure

All endpoints return a JSON response with the following top-level structure:

```json
{
  "ac": [],
  "msg": "No error",
  "now": 1695420989961,
  "total": 1,
  "ctime": 1695420989961,
  "ptime": 0
}
```

## Response Metadata

| Field | Type    | Description                                |
| ----- | ------- | ------------------------------------------ |
| msg   | string  | Error message or status message            |
| now   | integer | Response generation timestamp (Unix epoch) |
| total | integer | Number of aircraft returned                |
| ctime | integer | Cache timestamp                            |
| ptime | integer | Processing time in milliseconds            |
| ac    | array   | List of aircraft objects                   |

---

# Endpoints

## Get Aircraft by ICAO Hex

```http
GET /hex/{hex}
```

### Example

```http
GET https://api.airplanes.live/v2/hex/45211e
```

### Parameters

| Parameter | Type   | Description                  |
| --------- | ------ | ---------------------------- |
| hex       | string | 24-bit ICAO aircraft address |

---

## Get Aircraft by Callsign

```http
GET /callsign/{callsign}
```

### Example

```http
GET /callsign/CFG846
```

Returns aircraft matching the specified callsign.

---

## Get Aircraft by Registration

```http
GET /reg/{registration}
```

### Example

```http
GET /reg/LZ-LAJ
```

Returns aircraft matching the specified registration.

---

## Get Aircraft by ICAO Aircraft Type

```http
GET /type/{icaoType}
```

### Example

```http
GET /type/A320
```

Returns aircraft matching the ICAO type code.

Common examples:

* A320
* A321
* B738
* B77W
* E190

---

## Get Aircraft by Squawk

```http
GET /squawk/{code}
```

### Example

```http
GET /squawk/7700
```

Returns aircraft currently using the specified squawk code.

---

## Get Military Aircraft

```http
GET /mil
```

Returns aircraft flagged as military.

---

## Get LADD Aircraft

```http
GET /ladd
```

Returns aircraft flagged as LADD (Limiting Aircraft Data Displayed).

---

## Get PIA Aircraft

```http
GET /pia
```

Returns aircraft flagged as PIA.

---

## Get Aircraft Near a Geographic Point

```http
GET /point/{latitude}/{longitude}/{radius}
```

### Parameters

| Parameter | Type    | Description                           |
| --------- | ------- | ------------------------------------- |
| latitude  | float   | Latitude                              |
| longitude | float   | Longitude                             |
| radius    | integer | Radius in nautical miles (max 250 NM) |

### Example

```http
GET /point/25.7617/-80.1918/50
```

Returns aircraft within 50 nautical miles of Miami.

---

# Aircraft Object Schema

Each aircraft entry inside the `ac` array contains available aircraft telemetry.

## Identity

| Field  | Type   | Description                  |
| ------ | ------ | ---------------------------- |
| hex    | string | ICAO 24-bit aircraft address |
| r      | string | Registration                 |
| flight | string | Callsign                     |
| t      | string | ICAO aircraft type           |
| desc   | string | Aircraft description         |
| type   | string | Data source type             |

---

## Position

| Field    | Type           | Unit                |
| -------- | -------------- | ------------------- |
| lat      | float          | Degrees             |
| lon      | float          | Degrees             |
| alt_baro | integer/string | Feet or "ground"    |
| alt_geom | integer        | Feet                |
| rr_lat   | float          | Estimated latitude  |
| rr_lon   | float          | Estimated longitude |
| track    | float          | Degrees             |

---

## Speed

| Field      | Type    | Unit        |
| ---------- | ------- | ----------- |
| gs         | integer | Knots       |
| ias        | integer | Knots       |
| tas        | integer | Knots       |
| mach       | float   | Mach        |
| track_rate | float   | Degrees/sec |

---

## Heading & Attitude

| Field        | Type  | Unit    |
| ------------ | ----- | ------- |
| roll         | float | Degrees |
| mag_heading  | float | Degrees |
| true_heading | float | Degrees |

---

## Vertical Performance

| Field     | Type    | Unit        |
| --------- | ------- | ----------- |
| baro_rate | integer | Feet/minute |
| geom_rate | integer | Feet/minute |

---

## Flight Management

| Field            | Type    | Description                |
| ---------------- | ------- | -------------------------- |
| nav_qnh          | float   | Altimeter setting (hPa)    |
| nav_altitude_mcp | integer | Selected altitude from MCP |
| nav_altitude_fms | integer | Selected altitude from FMS |
| nav_heading      | float   | Selected heading           |
| nav_modes        | array   | Active automation modes    |

### nav_modes Values

```text
autopilot
vnav
althold
approach
lnav
tcas
```

---

## Transponder Information

| Field     | Type   | Description            |
| --------- | ------ | ---------------------- |
| squawk    | string | Four-digit squawk code |
| emergency | string | Emergency state        |
| category  | string | Aircraft category      |

### Emergency Values

```text
none
general
lifeguard
minfuel
nordo
unlawful
downed
reserved
```

---

## Surveillance Quality

| Field    | Type    |
| -------- | ------- |
| nic      | integer |
| nic_baro | integer |
| nac_p    | integer |
| nac_v    | integer |
| sil      | integer |
| sil_type | string  |
| gva      | integer |
| sda      | integer |
| rc       | integer |

---

## Telemetry Status

| Field    | Type    | Description                     |
| -------- | ------- | ------------------------------- |
| messages | integer | Total messages received         |
| seen     | float   | Seconds since last message      |
| seen_pos | float   | Seconds since last position     |
| rssi     | float   | Signal strength (dBFS)          |
| alert    | integer | Alert bit                       |
| spi      | integer | Special Position Identification |

---

## Weather Estimates

| Field | Type    | Unit                         |
| ----- | ------- | ---------------------------- |
| wd    | integer | Wind direction               |
| ws    | integer | Wind speed                   |
| oat   | integer | Outside air temperature (°C) |
| tat   | integer | Total air temperature (°C)   |

---

# Database Flags

The `dbFlags` field is a bitmask.

| Flag        | Value |
| ----------- | ----- |
| Military    | 1     |
| Interesting | 2     |
| PIA         | 4     |
| LADD        | 8     |

### Example

```javascript
const isMilitary = (dbFlags & 1) !== 0;
const isInteresting = (dbFlags & 2) !== 0;
const isPIA = (dbFlags & 4) !== 0;
const isLADD = (dbFlags & 8) !== 0;
```

---

# Data Source Types

The `type` field identifies the source of aircraft data.

| Value          | Description                   |
| -------------- | ----------------------------- |
| adsb_icao      | ADS-B with ICAO address       |
| adsb_icao_nt   | ADS-B non-transponder emitter |
| adsr_icao      | ADS-R rebroadcast             |
| tisb_icao      | TIS-B target                  |
| adsc           | ADS-C satellite reports       |
| mlat           | Multilateration position      |
| mode_s         | Mode-S transponder            |
| adsb_other     | ADS-B non-ICAO address        |
| adsr_other     | ADS-R non-ICAO address        |
| tisb_other     | TIS-B non-ICAO address        |
| tisb_trackfile | Radar track/file target       |
| other          | Unknown source                |

---

# Last Known Position

When current position data is stale, a `lastPosition` object may be returned.

```json
{
  "lastPosition": {
    "lat": 43.261414,
    "lon": 29.636404,
    "nic": 8,
    "rc": 185,
    "seen_pos": 3061.406
  }
}
```

## Fields

| Field    | Type    |
| -------- | ------- |
| lat      | float   |
| lon      | float   |
| nic      | integer |
| rc       | integer |
| seen_pos | float   |

---

# Example Aircraft Object

```json
{
  "hex": "45211e",
  "flight": "CFG846",
  "r": "LZ-LAJ",
  "t": "A320",
  "alt_baro": 37000,
  "gs": 496,
  "track": 113.55,
  "lat": 43.261414,
  "lon": 29.636404,
  "squawk": "7665",
  "messages": 7675,
  "seen": 0.5
}
```

---

# AI Usage Notes

When consuming this API:

1. Never assume all fields are present.
2. Use `hex` as the primary aircraft identifier.
3. Use `flight` when displaying human-readable flight information.
4. Prefer `lat` and `lon` for current position.
5. Fall back to `lastPosition` when current coordinates are unavailable.
6. Use `seen` and `seen_pos` to determine data freshness.
7. Interpret `dbFlags` as a bitmask.
8. Aircraft may disappear when position updates become stale.
9. `alt_baro` may be either a number or the string `"ground"`.
10. Values derived from MLAT or estimated positions may be less accurate than ADS-B positions.

```
```
