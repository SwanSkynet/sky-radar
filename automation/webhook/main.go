package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log"
	"net/http"
	"os"

	"automation/workflow"

	"go.temporal.io/sdk/client"
)

// webhookSecret, if set via GITHUB_WEBHOOK_SECRET, is used to verify the
// X-Hub-Signature-256 header GitHub sends on every delivery. This endpoint
// is meant to sit behind a public ngrok tunnel, so without this check
// anyone who finds the URL could fire spurious review-submitted signals.
var webhookSecret = os.Getenv("GITHUB_WEBHOOK_SECRET")

func validSignature(body []byte, header string) bool {
	if webhookSecret == "" {
		return true // no secret configured; skip verification
	}
	const prefix = "sha256="
	if len(header) <= len(prefix) || header[:len(prefix)] != prefix {
		return false
	}
	mac := hmac.New(sha256.New, []byte(webhookSecret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(header[len(prefix):]), []byte(expected))
}

func main() {
	if webhookSecret == "" {
		log.Println("WARNING: GITHUB_WEBHOOK_SECRET not set; webhook signature verification is disabled")
	}

	c, err := client.Dial(client.Options{HostPort: "localhost:7233"})
	if err != nil {
		log.Fatalln("unable to create client:", err)
	}
	defer c.Close()

	http.HandleFunc("/webhook", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}

		if !validSignature(body, r.Header.Get("X-Hub-Signature-256")) {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}

		event := r.Header.Get("X-GitHub-Event")
		log.Println("Received GitHub event:", event)

		if event == "pull_request_review" {
			err := c.SignalWorkflow(context.Background(),
				"claude-pipeline-run", // workflow ID — must match starter/main.go
				"",
				workflow.ReviewSignalChannel,
				nil,
			)
			if err != nil {
				log.Println("signal error:", err)
				http.Error(w, "signal failed", http.StatusInternalServerError)
				return
			}
		}

		w.WriteHeader(http.StatusOK)
	})

	log.Println("Webhook receiver listening on :8090/webhook")
	log.Fatal(http.ListenAndServe(":8090", nil))
}
