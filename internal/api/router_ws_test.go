package api

import (
	"testing"

	"github.com/gin-gonic/gin"
)

// NewRouter must register the WS route alongside the /v1 group without a gin
// route conflict (registration panics on conflict), and the route must be the
// path the gateway rewrites to (/v1/ws/events).
func TestNewRouterRegistersWSRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := NewRouter(RouterDeps{}) // zero deps: registration alone must not panic

	found := false
	for _, ri := range r.Routes() {
		if ri.Method == "GET" && ri.Path == "/v1/ws/events" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("GET /v1/ws/events was not registered")
	}
}
