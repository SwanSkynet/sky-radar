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

const providerName = "airplanes.live"

// aircraftResponse mirrors the airplanes.live /v2 response envelope. See
// docs/api-docs/airplanes-live-docs.md.
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

// Client polls the airplanes.live /point endpoint and maps each aircraft
// entry into a sourceadapter.RawState. It implements sourceadapter.Adapter.
type Client struct {
	httpClient  *http.Client
	baseURL     string
	lat, lon    float64
	radiusNM    int
	backoff     *sourceadapter.Backoff
	maxAttempts int
}

// NewClient returns a Client that queries baseURL's /point endpoint
// centered on (lat, lon) with the given radius in nautical miles (max 250
// per the provider's documented limit).
func NewClient(httpClient *http.Client, baseURL string, lat, lon float64, radiusNM int) *Client {
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
	url := fmt.Sprintf("%s/point/%s/%s/%d", c.baseURL, formatCoord(c.lat), formatCoord(c.lon), c.radiusNM)

	var body []byte
	err := sourceadapter.Retry(ctx, c.backoff, c.maxAttempts, func() error {
		b, fetchErr := c.fetch(ctx, url)
		if fetchErr == nil {
			body = b
		}
		return fetchErr
	})
	if err != nil {
		return nil, fmt.Errorf("airplaneslive: poll: %w", err)
	}

	var resp aircraftResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("airplaneslive: decode response: %w", err)
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError {
		return nil, &sourceadapter.RetryableError{
			Err:        fmt.Errorf("airplaneslive: status %d", resp.StatusCode),
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
		}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("airplaneslive: unexpected status %d", resp.StatusCode)
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
