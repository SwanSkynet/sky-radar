package main

import (
	"net/http"
	"strings"
)

// sessionHeader carries the anonymous, client-generated session identifier
// that scopes zones and watchlist entries to their creator, per
// docs/architecture/data-model.md's "no account system required" design:
// there is no login, so the client is responsible for generating and
// persisting (e.g. in localStorage) a stable opaque ID and sending it on
// every zones/watchlist request.
const sessionHeader = "X-Session-ID"

// requireSession reads sessionHeader from r, writing a 400 response and
// returning ok=false if it's missing.
func requireSession(w http.ResponseWriter, r *http.Request) (session string, ok bool) {
	session = strings.TrimSpace(r.Header.Get(sessionHeader))
	if session == "" {
		writeError(w, http.StatusBadRequest, sessionHeader+" header is required")
		return "", false
	}
	return session, true
}
