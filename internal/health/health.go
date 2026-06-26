// Package health provides the liveness and readiness HTTP handlers shared
// by every Phase 1 service, so the Redis-dependency contract (timeout,
// status codes, body text) can't drift between binaries.
package health

import (
	"context"
	"net/http"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/redisutil"
)

// pingTimeout bounds how long Ready waits on Redis before reporting
// unhealthy, so a hung connection can't stall the check indefinitely.
const pingTimeout = 2 * time.Second

// Live always reports 200 once the process is serving HTTP, with no
// downstream dependency check. A Fly health check (or the soak-test
// monitor in scripts/soak-test.sh) bound to this endpoint won't
// restart-loop an otherwise-healthy process during a transient Redis
// outage — see Ready for that signal instead.
func Live(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// Ready reports unhealthy (503) if redisClient is unreachable, so an
// orchestrator (or the soak-test monitor) can detect a Redis-dependency
// outage via this endpoint without that signal restarting the process
// through Live.
func Ready(redisClient *redisutil.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), pingTimeout)
		defer cancel()
		if err := redisClient.Ping(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("unhealthy: redis unreachable"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}
}
