package redisutil

import (
	"context"
	"testing"

	"github.com/redis/go-redis/v9"
)

func TestPingSucceedsAgainstLiveServer(t *testing.T) {
	c := newTestClient(t)
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestPingFailsAgainstUnreachableServer(t *testing.T) {
	c := New(&redis.Options{Addr: "127.0.0.1:1"})
	defer func() { _ = c.Close() }()

	if err := c.Ping(context.Background()); err == nil {
		t.Fatal("Ping returned nil error, want error for unreachable server")
	}
}
