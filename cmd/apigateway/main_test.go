package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SwanSkynet/sky-radar/internal/redisutil"
	"github.com/redis/go-redis/v9"
)

func TestHealthzOKWhenRedisReachable(t *testing.T) {
	api, _ := testAPI(t)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz(api.redis))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); got != "ok" {
		t.Fatalf("body = %q, want %q", got, "ok")
	}
}

func TestHealthzUnavailableWhenRedisUnreachable(t *testing.T) {
	redisClient := redisutil.New(&redis.Options{Addr: "127.0.0.1:0"})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz(redisClient))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}
