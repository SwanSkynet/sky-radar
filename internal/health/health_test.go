package health

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SwanSkynet/sky-radar/internal/redisutil"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func testRedisClient(t *testing.T) *redisutil.Client {
	t.Helper()
	mr := miniredis.RunT(t)
	return redisutil.New(&redis.Options{Addr: mr.Addr()})
}

func TestLiveAlwaysOK(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	Live(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); got != "ok" {
		t.Fatalf("body = %q, want %q", got, "ok")
	}
}

func TestReadyOKWhenRedisReachable(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	Ready(testRedisClient(t))(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); got != "ok" {
		t.Fatalf("body = %q, want %q", got, "ok")
	}
}

func TestReadyUnavailableWhenRedisUnreachable(t *testing.T) {
	redisClient := redisutil.New(&redis.Options{Addr: "127.0.0.1:0"})

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	Ready(redisClient)(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}
