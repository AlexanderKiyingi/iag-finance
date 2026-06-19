package api

import (
	"strings"
	"testing"

	"github.com/iag-finance/backend/internal/permissions"
)

// TestRouteGatesMatchRouter asserts the declarative gate table
// (permissions.RouteGates) and the actual registered mutating routes are in
// exact correspondence — every mutating route has a gate, and every declared
// gate maps to a real route. The gatedGroup registrar already panics at startup
// for a missing gate; this catches the reverse (a stale/orphan gate entry) and
// documents the invariant.
func TestRouteGatesMatchRouter(t *testing.T) {
	r := NewRouter(RouterDeps{}) // route registration does no I/O

	registered := map[string]bool{}
	for _, ri := range r.Routes() {
		switch ri.Method {
		case "POST", "PUT", "PATCH", "DELETE":
			registered[ri.Method+" "+strings.TrimPrefix(ri.Path, "/v1")] = true
		}
	}

	declared := map[string]bool{}
	for _, g := range permissions.RouteGates() {
		declared[g.Method+" "+g.Path] = true
	}

	for k := range declared {
		if !registered[k] {
			t.Errorf("gate declared for %q but no such mutating route is registered", k)
		}
	}
	for k := range registered {
		if !declared[k] {
			t.Errorf("mutating route %q is registered but has no gate in permissions.RouteGates()", k)
		}
	}
}
