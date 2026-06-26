package main

import (
	_ "embed"
	"net/http"
)

// openAPISpecYAML is the published public API v1 schema, embedded so the
// binary serves exactly the file checked into source control — there is
// no separate build step that could let the served copy drift from the
// one the contract tests in contract_test.go validate against.
//
//go:embed openapi-v1.yaml
var openAPISpecYAML []byte

// serveOpenAPISpec handles GET /api/v1/openapi.yaml. It is intentionally
// outside apiAuth's middleware (see newRouterWithExtras): a caller has to
// be able to fetch the schema before it can know how auth/rate limiting
// even work.
func serveOpenAPISpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(openAPISpecYAML)
}
