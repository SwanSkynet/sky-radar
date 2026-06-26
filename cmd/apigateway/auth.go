package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
	"github.com/SwanSkynet/sky-radar/internal/pgstore"
	"github.com/SwanSkynet/sky-radar/internal/redisutil"
)

// apiKeyHeader carries the caller's elevated-tier credential, per
// docs/tech-stack/backend.md's "Owns API-key auth, per-key rate limiting"
// and docs/architecture/data-model.md's API authentication section. Its
// absence is not an error — the request is simply served at the
// anonymous tier.
const apiKeyHeader = "X-API-Key"

// apiTier names a public API v1 rate-limit tier per
// docs/prd/phase-2-realtime-systems.md's "API-key auth for elevated rate
// limits, anonymous tier for casual use".
type apiTier string

const (
	tierAnonymous apiTier = "anonymous"
	tierElevated  apiTier = "elevated"
)

// tierLimit is a token bucket's (capacity, refill rate) pair for one tier.
type tierLimit struct {
	capacity     int
	refillPerSec float64
}

// newTierLimit derives a tierLimit from a requests-per-minute budget: the
// bucket's burst capacity equals one minute's allowance, and it refills at
// that same rate spread evenly across the minute, so a client well under
// budget never gets throttled by burstiness alone.
func newTierLimit(perMinute int) tierLimit {
	return tierLimit{capacity: perMinute, refillPerSec: float64(perMinute) / 60.0}
}

// apiAuth implements the public API v1 auth/rate-limit middleware: every
// request either presents a valid apiKeyHeader (elevated tier, limited
// per-key) or is served anonymously (limited per-IP), per
// docs/architecture/data-model.md. It owns no other request-handling
// concerns — business logic stays in the wrapped handlers.
type apiAuth struct {
	pg             *pgstore.Store
	redis          *redisutil.Client
	logger         *slog.Logger
	anonymousLimit tierLimit
	elevatedLimit  tierLimit
}

// middleware wraps next so every request is authenticated (if it presents
// an API key) and rate-limited before reaching next. A 401 is returned for
// an unrecognized or revoked key; a 429 with a Retry-After header is
// returned once the caller's tier-appropriate token bucket is exhausted,
// per docs/prd/phase-2-realtime-systems.md P2-FR8's "rate-limit
// enforcement test (429 + Retry-After)" acceptance criterion.
func (a *apiAuth) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := a.anonymousLimit
		bucketKey := "ratelimit:ip:" + clientIP(r)

		if rawKey := strings.TrimSpace(r.Header.Get(apiKeyHeader)); rawKey != "" {
			key, err := a.pg.LookupAPIKeyByHash(r.Context(), hashAPIKey(rawKey))
			if err != nil {
				if errors.Is(err, pgstore.ErrAPIKeyNotFound) {
					writeError(w, http.StatusUnauthorized, "invalid or revoked API key")
					return
				}
				a.logger.Error("api key lookup failed", "err", err)
				writeError(w, http.StatusInternalServerError, "failed to validate API key")
				return
			}
			// Only an elevated-tier key promotes the request to the
			// elevated bucket — an anonymous-tier key is still a valid
			// credential (e.g. for attribution), but stays on the same
			// per-IP bucket/limit as an unauthenticated request.
			if key.Tier == string(tierElevated) {
				limit = a.elevatedLimit
				bucketKey = "ratelimit:key:" + key.ID
			}
		}

		if !a.enforceLimit(w, r, limit, bucketKey) {
			return
		}

		next.ServeHTTP(w, r)
	})
}

// rateLimitAnonymous wraps next with only the anonymous-tier per-IP rate
// limit, skipping API-key lookup entirely. It's for routes — like the
// published OpenAPI spec — that must stay reachable without a key but
// shouldn't be exempt from abuse protection.
func (a *apiAuth) rateLimitAnonymous(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.enforceLimit(w, r, a.anonymousLimit, "ratelimit:ip:"+clientIP(r)) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

// enforceLimit checks the token bucket for limit/bucketKey, setting the
// X-RateLimit-* response headers in all cases. It returns false (having
// already written a 429 or 500 response) when the caller must not proceed.
func (a *apiAuth) enforceLimit(w http.ResponseWriter, r *http.Request, limit tierLimit, bucketKey string) bool {
	result, err := a.redis.AllowTokenBucket(r.Context(), bucketKey, limit.capacity, limit.refillPerSec)
	if err != nil {
		a.logger.Error("rate limit check failed", "err", err)
		writeError(w, http.StatusInternalServerError, "rate limit check failed")
		return false
	}

	w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit.capacity))
	w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(result.Remaining))
	if !result.Allowed {
		retryAfterSec := int(math.Ceil(result.RetryAfter.Seconds()))
		w.Header().Set("Retry-After", strconv.Itoa(retryAfterSec))
		writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
		return false
	}
	return true
}

// clientIP returns the request's originating client address, used as the
// anonymous-tier rate-limit bucket key. apigateway only ever receives
// traffic via Caddy on the documented deployment topology (see
// docs/tech-stack/hosting-and-deployment.md: only Caddy's ports are
// publicly exposed), and Caddy's reverse_proxy appends the address it
// actually accepted the connection from as the last X-Forwarded-For
// element — so the last element is the one hop we trust; any earlier
// elements are caller-supplied and must not be used as the rate-limit
// identity, or a client could spoof a fresh bucket on every request by
// sending its own X-Forwarded-For. RemoteAddr is the fallback for direct
// (e.g. local dev) traffic with no proxy in front at all.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.LastIndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[i+1:])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// hashAPIKey returns the SHA-256 hex digest of a raw API key, matching
// internal/pgstore.APIKey.KeyHash. API keys are generated with
// generateAPIKey (32 random bytes, far more entropy than any password
// hashing scheme is defending against), so a fast, unsalted hash is
// sufficient here — the threat being defended against is "leaked database
// row", not "offline brute force of a guessable secret".
func hashAPIKey(rawKey string) string {
	sum := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(sum[:])
}

// apiKeyByteLength is the random-byte length of a generated API key,
// before hex-encoding doubles it.
const apiKeyByteLength = 32

// generateAPIKey returns a fresh, high-entropy raw API key, prefixed so a
// key is recognizable as such (e.g. in logs or accidental commits) without
// revealing anything about its hash.
func generateAPIKey() (string, error) {
	buf := make([]byte, apiKeyByteLength)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate api key: %w", err)
	}
	return "skr_" + hex.EncodeToString(buf), nil
}

// issueAPIKey generates a new elevated- or anonymous-tier key, persists
// its hash via pg, and prints the raw key to stdout exactly once — it is
// never recoverable after this call returns. This backs cmd/apigateway's
// -issue-key startup flag (see main.go); it is deliberately not exposed as
// an HTTP endpoint, since the public API v1 surface has no account system
// to authorize who may mint a key (see docs/prd/00-master-prd.md's "no
// user accounts" scope decision).
func issueAPIKey(ctx context.Context, pg *pgstore.Store, label string, tier apiTier) (string, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		return "", errors.New("label is required")
	}
	if tier != tierAnonymous && tier != tierElevated {
		return "", fmt.Errorf("tier must be %q or %q", tierAnonymous, tierElevated)
	}

	rawKey, err := generateAPIKey()
	if err != nil {
		return "", err
	}

	record := pgstore.APIKey{
		ID:        flightmodel.NewID(),
		KeyHash:   hashAPIKey(rawKey),
		Label:     label,
		Tier:      string(tier),
		CreatedAt: time.Now().UTC(),
	}
	if err := pg.InsertAPIKey(ctx, record); err != nil {
		return "", fmt.Errorf("insert api key: %w", err)
	}

	fmt.Printf("Issued %s-tier API key %q:\n\n  %s\n\nStore this securely — it cannot be retrieved again; only its hash is kept.\n", tier, label, rawKey)
	return rawKey, nil
}
