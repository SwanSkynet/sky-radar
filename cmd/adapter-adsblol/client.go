package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/sourceadapter"
)

const (
	providerName = "adsb.lol"

	// maxResponseBodyBytes bounds how much of an upstream response the
	// adapter will buffer, so a malformed or abusive response can't
	// exhaust process memory.
	maxResponseBodyBytes = 5 << 20 // 5 MiB
)

// aircraftResponse mirrors the adsb.lol /v2 response envelope, which
// follows the same readsb-derived shape as airplanes.live's. See
// docs/api-docs/adsb-lol-api-docs.md.
type aircraftResponse struct {
	AC    []json.RawMessage `json:"ac"`
	Msg   string            `json:"msg"`
	Total int               `json:"total"`
}

// aircraftIdentity is the subset of fields the adapter reads to extract an
// identifier; everything else stays untouched in the raw payload for the
// normalizer to interpret per docs/api-docs/README.md's field mapping.
type aircraftIdentity struct {
	Hex string `json:"hex"`
}

// Client polls the adsb.lol /v2/lat/.../lon/.../dist/... endpoint and maps
// each aircraft entry into a sourceadapter.RawState. It implements
// sourceadapter.Adapter.
//
// adsb.lol does not require authentication today, but the provider has
// signaled API keys may become required in the future (see
// docs/api-docs/README.md); fetch is the single place a future auth header
// would be added, so that remains a config change rather than a rewrite.
type Client struct {
	httpClient  *http.Client
	baseURL     string
	lat, lon    float64
	radiusNM    int
	backoff     *sourceadapter.Backoff
	maxAttempts int
}

// NewClient returns a Client that queries baseURL's /v2/lat/.../lon/.../dist
// endpoint centered on (lat, lon) with the given radius in nautical miles,
// clamped to the provider's documented range of 1-250 NM.
func NewClient(httpClient *http.Client, baseURL string, lat, lon float64, radiusNM int) *Client {
	if radiusNM < 1 {
		radiusNM = 1
	}
	if radiusNM > 250 {
		radiusNM = 250
	}
	return &Client{
		httpClient:  httpClient,
		baseURL:     strings.TrimSuffix(baseURL, "/"),
		lat:         lat,
		lon:         lon,
		radiusNM:    radiusNM,
		backoff:     sourceadapter.NewBackoff(time.Second, 30*time.Second),
		maxAttempts: 5,
	}
}

// Poll fetches the current aircraft list and maps it into RawStates,
// retrying with backoff on 429/5xx per docs/tech-stack/backend.md.
func (c *Client) Poll(ctx context.Context) ([]sourceadapter.RawState, error) {
	url := fmt.Sprintf("%s/v2/lat/%s/lon/%s/dist/%d", c.baseURL, formatCoord(c.lat), formatCoord(c.lon), c.radiusNM)

	var body []byte
	err := sourceadapter.Retry(ctx, c.backoff, c.maxAttempts, func() error {
		b, fetchErr := c.fetch(ctx, url)
		if fetchErr == nil {
			body = b
		}
		return fetchErr
	})
	if err != nil {
		return nil, fmt.Errorf("adsblol: poll: %w", err)
	}

	var resp aircraftResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("adsblol: decode response: %w", err)
	}

	now := time.Now().UTC()
	states := make([]sourceadapter.RawState, 0, len(resp.AC))
	for _, raw := range resp.AC {
		var id aircraftIdentity
		if err := json.Unmarshal(raw, &id); err != nil || id.Hex == "" {
			continue
		}
		states = append(states, sourceadapter.RawState{
			Provider:  providerName,
			ICAO24:    strings.ToLower(id.Hex),
			FetchedAt: now,
			Payload:   raw,
		})
	}
	return states, nil
}

func (c *Client) fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxResponseBodyBytes {
		return nil, fmt.Errorf("adsblol: response exceeds %d bytes", maxResponseBodyBytes)
	}

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError {
		return nil, &sourceadapter.RetryableError{
			Err:        fmt.Errorf("adsblol: status %d", resp.StatusCode),
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
		}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("adsblol: unexpected status %d", resp.StatusCode)
	}
	return body, nil
}

func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	return 0
}

func formatCoord(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}
