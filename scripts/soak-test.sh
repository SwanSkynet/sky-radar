#!/usr/bin/env bash
# Polls every Phase 1 service's health endpoint (plus the public /flights
# route) on a fixed interval for the duration of an unattended soak test,
# logging every check to a file so a failure window can be reconstructed
# afterward instead of requiring someone to babysit the run.
#
# Works against either the local docker-compose stack (default) or a
# production deployment, by overriding the *_URL env vars below to the
# public Fly.io URLs. See docs/runbooks/soak-test.md for full usage.
#
# Usage:
#   ./scripts/soak-test.sh                 # 24h run, 60s interval
#   ./scripts/soak-test.sh --once          # single check pass, then exit
#   SOAK_DURATION_SECONDS=3600 ./scripts/soak-test.sh   # 1h run
set -euo pipefail

APIGATEWAY_URL="${APIGATEWAY_URL:-http://localhost:8080}"
NORMALIZER_URL="${NORMALIZER_URL:-http://localhost:8084}"
ADAPTER_OPENSKY_URL="${ADAPTER_OPENSKY_URL:-http://localhost:8081}"
ADAPTER_ADSBLOL_URL="${ADAPTER_ADSBLOL_URL:-http://localhost:8082}"
ADAPTER_AIRPLANESLIVE_URL="${ADAPTER_AIRPLANESLIVE_URL:-http://localhost:8083}"

SOAK_DURATION_SECONDS="${SOAK_DURATION_SECONDS:-86400}"
SOAK_INTERVAL_SECONDS="${SOAK_INTERVAL_SECONDS:-60}"
SOAK_REQUEST_TIMEOUT_SECONDS="${SOAK_REQUEST_TIMEOUT_SECONDS:-5}"
SOAK_LOG_FILE="${SOAK_LOG_FILE:-soak-test-$(date +%Y%m%d-%H%M%S).log}"

RUN_ONCE=false
if [[ "${1:-}" == "--once" ]]; then
  RUN_ONCE=true
fi

# Parallel indexed arrays instead of associative arrays for compatibility
# with the bash 3.2 shipped on macOS (no Bash 4+ dependency required).
#
# apigateway-healthz hits /healthz, which is pure process liveness (see
# internal/health.Live) — it can't see a Redis outage. The other four
# targets hit /readyz instead (internal/health.Ready), which is the one
# that actually pings Redis, so a real Redis outage during the soak
# window still shows up as DOWN lines per P1-FR7.
NAMES=(apigateway-healthz normalizer-readyz adapter-opensky-readyz adapter-adsblol-readyz adapter-airplaneslive-readyz apigateway-flights)
URLS=(
  "$APIGATEWAY_URL/healthz"
  "$NORMALIZER_URL/readyz"
  "$ADAPTER_OPENSKY_URL/readyz"
  "$ADAPTER_ADSBLOL_URL/readyz"
  "$ADAPTER_AIRPLANESLIVE_URL/readyz"
  "$APIGATEWAY_URL/flights?bbox=-90,-180,90,180"
)
FLIGHTS_IDX=$((${#NAMES[@]} - 1))
TOTAL=()
FAIL=()
STREAK=()
MAXSTREAK=()
WAS_UP=()
for i in "${!NAMES[@]}"; do
  TOTAL[i]=0
  FAIL[i]=0
  STREAK[i]=0
  MAXSTREAK[i]=0
  WAS_UP[i]=1
done

# Tracks whether /flights ever returned a non-empty aircraft set, and
# whether that set ever changed across the run — a 200 with a stale or
# permanently empty body would otherwise pass the plain status check
# below while telling us nothing about whether data is actually flowing.
FLIGHTS_EVER_NONEMPTY=false
FLIGHTS_EVER_CHANGED=false
FLIGHTS_NONEMPTY_SAMPLES=0
LAST_FLIGHTS_FINGERPRINT=""

log() {
  echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) $*" | tee -a "$SOAK_LOG_FILE"
}

check_one() {
  local idx="$1"
  local url="${URLS[$idx]}"
  local code
  code=$(curl -s -o /dev/null -w '%{http_code}' --max-time "$SOAK_REQUEST_TIMEOUT_SECONDS" "$url") || code="000"

  TOTAL[idx]=$((TOTAL[idx] + 1))

  if [[ "$code" == "200" ]]; then
    STREAK[idx]=0
    if [[ "${WAS_UP[$idx]}" == "0" ]]; then
      log "RECOVERED ${NAMES[$idx]} ($url) -> $code"
    fi
    WAS_UP[idx]=1
  else
    FAIL[idx]=$((FAIL[idx] + 1))
    STREAK[idx]=$((STREAK[idx] + 1))
    if (( STREAK[idx] > MAXSTREAK[idx] )); then
      MAXSTREAK[idx]=${STREAK[idx]}
    fi
    if [[ "${WAS_UP[$idx]}" == "1" ]]; then
      log "DOWN ${NAMES[$idx]} ($url) -> $code"
    fi
    WAS_UP[idx]=0
  fi
}

# check_flights probes apigateway-flights like check_one, but a 200 only
# counts as up if the body actually contains a non-empty aircraft set —
# a stale or empty 200 would otherwise mask a frozen ingestion pipeline.
check_flights() {
  local idx="$1"
  local url="${URLS[$idx]}"
  local response code body
  response=$(curl -s -w '\n%{http_code}' --max-time "$SOAK_REQUEST_TIMEOUT_SECONDS" "$url") || response=$'\n000'
  code="${response##*$'\n'}"
  body="${response%$'\n'*}"

  TOTAL[idx]=$((TOTAL[idx] + 1))

  local fingerprint=""
  if [[ "$code" == "200" ]]; then
    # grep exits non-zero on an empty (legitimate) aircraft set; the `|| true`
    # keeps that from tripping `set -o pipefail` and aborting the script.
    fingerprint=$( { grep -o '"icao24":"[^"]*"' <<<"$body" || true; } | sort -u | tr '\n' ',')
  fi

  if [[ "$code" == "200" && -n "$fingerprint" ]]; then
    FLIGHTS_EVER_NONEMPTY=true
    FLIGHTS_NONEMPTY_SAMPLES=$((FLIGHTS_NONEMPTY_SAMPLES + 1))
    if [[ -n "$LAST_FLIGHTS_FINGERPRINT" && "$fingerprint" != "$LAST_FLIGHTS_FINGERPRINT" ]]; then
      FLIGHTS_EVER_CHANGED=true
    fi
    LAST_FLIGHTS_FINGERPRINT="$fingerprint"

    STREAK[idx]=0
    if [[ "${WAS_UP[$idx]}" == "0" ]]; then
      log "RECOVERED ${NAMES[$idx]} ($url) -> $code"
    fi
    WAS_UP[idx]=1
  else
    FAIL[idx]=$((FAIL[idx] + 1))
    STREAK[idx]=$((STREAK[idx] + 1))
    if (( STREAK[idx] > MAXSTREAK[idx] )); then
      MAXSTREAK[idx]=${STREAK[idx]}
    fi
    if [[ "${WAS_UP[$idx]}" == "1" ]]; then
      if [[ "$code" == "200" ]]; then
        log "DOWN ${NAMES[$idx]} ($url) -> $code (empty aircraft set)"
      else
        log "DOWN ${NAMES[$idx]} ($url) -> $code"
      fi
    fi
    WAS_UP[idx]=0
  fi
}

print_summary() {
  log "==== soak test summary ===="
  local overall_pass=true
  for i in "${!NAMES[@]}"; do
    local total=${TOTAL[$i]}
    local fail=${FAIL[$i]}
    local uptime_pct="n/a"
    if (( total > 0 )); then
      uptime_pct=$(awk -v t="$total" -v f="$fail" 'BEGIN { printf "%.2f", (t - f) / t * 100 }')
    fi
    log "${NAMES[$i]}: checks=$total failures=$fail uptime=${uptime_pct}% longest_outage_checks=${MAXSTREAK[$i]} (~$((MAXSTREAK[i] * SOAK_INTERVAL_SECONDS))s)"
    if (( fail > 0 )); then
      overall_pass=false
    fi
  done

  log "apigateway-flights data: ever_non_empty=$FLIGHTS_EVER_NONEMPTY ever_changed=$FLIGHTS_EVER_CHANGED non_empty_samples=$FLIGHTS_NONEMPTY_SAMPLES"
  if ! $FLIGHTS_EVER_NONEMPTY; then
    log "RESULT NOTE: /flights never returned a non-empty aircraft set during this run"
    overall_pass=false
  elif (( FLIGHTS_NONEMPTY_SAMPLES >= 2 )) && ! $FLIGHTS_EVER_CHANGED; then
    log "RESULT NOTE: /flights returned the same aircraft set on every non-empty check; data may be stale"
    overall_pass=false
  fi

  if $overall_pass; then
    log "RESULT: PASS (zero failed checks across all targets, and /flights data is flowing)"
  else
    log "RESULT: FAIL (one or more targets had failed checks, or /flights data isn't flowing; see DOWN/RECOVERED lines above for windows, and check service logs for sustained 429s per P1-FR2)"
  fi
}

trap 'print_summary; exit 0' INT TERM

log "soak test starting: duration=${SOAK_DURATION_SECONDS}s interval=${SOAK_INTERVAL_SECONDS}s log=$SOAK_LOG_FILE"

start_ts=$(date +%s)
while true; do
  for i in "${!NAMES[@]}"; do
    if (( i == FLIGHTS_IDX )); then
      check_flights "$i"
    else
      check_one "$i"
    fi
  done

  if $RUN_ONCE; then
    break
  fi

  now_ts=$(date +%s)
  if (( now_ts - start_ts >= SOAK_DURATION_SECONDS )); then
    break
  fi

  sleep "$SOAK_INTERVAL_SECONDS"
done

print_summary
