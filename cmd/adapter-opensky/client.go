package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/sourceadapter"
)

const (
	providerName = "opensky"

	// maxResponseBodyBytes bounds how much of an upstream response the
	// adapter will buffer, so a malformed or abusive response can't
	// exhaust process memory.
	maxResponseBodyBytes = 5 << 20 // 5 MiB

	// tokenExpirysafetyMargin is subtracted from the OAuth2 token's
	// reported lifetime so the adapter refreshes slightly before the
	// provider actually expires it.
	tokenExpiryMargin = 30 * time.Second
)

// statesResponse mirrors the OpenSky /states/all response envelope. Each
// entry in States is a positional JSON array (not an object); see
// docs/api-docs/opensky-api-docs.md for the field order.
type statesResponse struct {
	Time   int64             `json:"time"`
	States []json.RawMessage `json:"states"`
}

// BoundingBox restricts a /states/all query to a geographic area, per the
// lamin/lomin/lamax/lomax parameters in docs/api-docs/opensky-api-docs.md.
type BoundingBox struct {
	LaMin, LoMin, LaMax, LoMax float64
}

// Client polls the OpenSky /states/all endpoint and maps each state vector
// into a sourceadapter.RawState. It implements sourceadapter.Adapter.
//
// OAuth2 client-credentials auth is applied only when ClientID/ClientSecret
// are configured: /states/all also works anonymously, just with a lower
// rate limit (see docs/api-docs/README.md).
type Client struct {
	httpClient   *http.Client
	baseURL      string
	authURL      string
	clientID     string
	clientSecret string
	bbox         *BoundingBox
	backoff      *sourceadapter.Backoff
	maxAttempts  int

	tokenMu     sync.Mutex
	cachedToken string
	tokenExpiry time.Time
}

// NewClient returns a Client that queries baseURL's /states/all endpoint,
// optionally restricted to bbox and authenticated via clientID/clientSecret
// against authURL. Pass an empty clientID to query anonymously, and a nil
// bbox to query the entire network.
func NewClient(httpClient *http.Client, baseURL, authURL, clientID, clientSecret string, bbox *BoundingBox) *Client {
	return &Client{
		httpClient:   httpClient,
		baseURL:      strings.TrimSuffix(baseURL, "/"),
		authURL:      authURL,
		clientID:     clientID,
		clientSecret: clientSecret,
		bbox:         bbox,
		backoff:      sourceadapter.NewBackoff(time.Second, 30*time.Second),
		maxAttempts:  5,
	}
}

// Poll fetches the current state vectors and maps them into RawStates,
// retrying with backoff on 429/5xx per docs/tech-stack/backend.md.
func (c *Client) Poll(ctx context.Context) ([]sourceadapter.RawState, error) {
	reqURL := c.baseURL + "/states/all"
	if c.bbox != nil {
		q := url.Values{}
		q.Set("lamin", formatCoord(c.bbox.LaMin))
		q.Set("lomin", formatCoord(c.bbox.LoMin))
		q.Set("lamax", formatCoord(c.bbox.LaMax))
		q.Set("lomax", formatCoord(c.bbox.LoMax))
		reqURL += "?" + q.Encode()
	}

	var token string
	if c.clientID != "" {
		t, err := c.getToken(ctx)
		if err != nil {
			return nil, fmt.Errorf("opensky: get token: %w", err)
		}
		token = t
	}

	var body []byte
	err := sourceadapter.Retry(ctx, c.backoff, c.maxAttempts, func() error {
		b, fetchErr := c.fetch(ctx, reqURL, token)
		if fetchErr == nil {
			body = b
		}
		return fetchErr
	})
	if err != nil {
		return nil, fmt.Errorf("opensky: poll: %w", err)
	}

	var resp statesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("opensky: decode response: %w", err)
	}

	now := time.Now().UTC()
	states := make([]sourceadapter.RawState, 0, len(resp.States))
	for _, raw := range resp.States {
		icao24, ok := stateVectorICAO24(raw)
		if !ok || icao24 == "" {
			continue
		}
		states = append(states, sourceadapter.RawState{
			Provider:  providerName,
			ICAO24:    strings.ToLower(icao24),
			FetchedAt: now,
			Payload:   raw,
		})
	}
	return states, nil
}

// stateVectorICAO24 extracts index 0 (icao24) from a state vector's
// positional JSON array without decoding the rest of the fields.
func stateVectorICAO24(raw json.RawMessage) (string, bool) {
	var fields []json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || len(fields) == 0 {
		return "", false
	}
	var icao24 string
	if err := json.Unmarshal(fields[0], &icao24); err != nil {
		return "", false
	}
	return icao24, true
}

func (c *Client) fetch(ctx context.Context, reqURL, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
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
		return nil, fmt.Errorf("opensky: response exceeds %d bytes", maxResponseBodyBytes)
	}

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError {
		return nil, &sourceadapter.RetryableError{
			Err:        fmt.Errorf("opensky: status %d", resp.StatusCode),
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
		}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("opensky: unexpected status %d", resp.StatusCode)
	}
	return body, nil
}

// oauthToken mirrors the OpenSky/Keycloak client-credentials token
// response. See docs/api-docs/opensky-api-docs.md.
type oauthToken struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

// getToken returns a cached access token, fetching and caching a new one
// from authURL if the cached token is absent or about to expire.
func (c *Client) getToken(ctx context.Context) (string, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	if c.cachedToken != "" && time.Now().Before(c.tokenExpiry) {
		return c.cachedToken, nil
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", c.clientID)
	form.Set("client_secret", c.clientSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.authURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyBytes+1))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("opensky: token request: unexpected status %d", resp.StatusCode)
	}

	var token oauthToken
	if err := json.Unmarshal(body, &token); err != nil {
		return "", fmt.Errorf("opensky: decode token response: %w", err)
	}
	if token.AccessToken == "" {
		return "", fmt.Errorf("opensky: token response missing access_token")
	}

	c.cachedToken = token.AccessToken
	c.tokenExpiry = time.Now().Add(time.Duration(token.ExpiresIn)*time.Second - tokenExpiryMargin)
	return c.cachedToken, nil
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
