package natsutil

import (
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
)

// startTestServer boots an in-process NATS server with JetStream enabled,
// storing state under a per-test temp dir, and returns its client URL.
// Test files calling this build their own *nats.Conn so each test controls
// its own connection lifecycle.
func startTestServer(t *testing.T) string {
	t.Helper()

	opts := &server.Options{
		Host:      "127.0.0.1",
		Port:      -1,
		JetStream: true,
		StoreDir:  t.TempDir(),
	}

	srv, err := server.NewServer(opts)
	if err != nil {
		t.Fatalf("server.NewServer: %v", err)
	}

	srv.Start()
	t.Cleanup(srv.Shutdown)

	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("nats test server: not ready for connections")
	}

	return srv.ClientURL()
}
