package redisutil

import (
	"context"

	"github.com/redis/go-redis/v9"
)

// Client wraps the shared Redis connection used for hot state, caching,
// and rate limiting. See docs/tech-stack/data-and-messaging.md.
type Client struct {
	rdb *redis.Client
}

// New returns a Client backed by a new go-redis connection using opts.
func New(opts *redis.Options) *Client {
	return &Client{rdb: redis.NewClient(opts)}
}

// Close releases the underlying Redis connection.
func (c *Client) Close() error {
	return c.rdb.Close()
}

// Ping verifies connectivity to Redis.
func (c *Client) Ping(ctx context.Context) error {
	return c.rdb.Ping(ctx).Err()
}
