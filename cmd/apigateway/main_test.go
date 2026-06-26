package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SwanSkynet/sky-radar/internal/redisutil"
	"github.com/redis/go-redis/v9"
)

func TestNewRouterHealthzOKRegardlessOfRedis(t *testing.T) {
	api, _ := testAPI(t)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	newRouter(api).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestNewRouterReadyzUnavailableWhenRedisUnreachable(t *testing.T) {
	api := &flightsAPI{redis: redisutil.New(&redis.Options{Addr: "127.0.0.1:0"})}

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	newRouter(api).ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}
