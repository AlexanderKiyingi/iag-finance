package api

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/iag-finance/backend/internal/middleware"
	"github.com/iag-finance/backend/internal/permissions"
)

// gatedGroup registers mutating routes with the RBAC gate sourced from
// permissions.RouteGates(), so the gate map is single-sourced and shared with
// the frontend manifest. Registering a mutating route that has no declared gate
// panics at startup (fail-closed) — surfacing omissions immediately rather than
// shipping an ungated write endpoint.
type gatedGroup struct {
	rg    *gin.RouterGroup
	gates map[string][]string
}

func newGatedGroup(rg *gin.RouterGroup) *gatedGroup {
	gates := make(map[string][]string, len(permissions.RouteGates()))
	for _, g := range permissions.RouteGates() {
		gates[g.Method+" "+g.Path] = g.Permissions
	}
	return &gatedGroup{rg: rg, gates: gates}
}

func (g *gatedGroup) handle(method, path string, h gin.HandlerFunc) {
	perms, ok := g.gates[method+" "+path]
	if !ok {
		panic(fmt.Sprintf("api: no RBAC gate declared for %s %s — add it to permissions.RouteGates()", method, path))
	}
	g.rg.Handle(method, path, middleware.Require(perms...), h)
}

func (g *gatedGroup) POST(path string, h gin.HandlerFunc)   { g.handle("POST", path, h) }
func (g *gatedGroup) PATCH(path string, h gin.HandlerFunc)  { g.handle("PATCH", path, h) }
func (g *gatedGroup) DELETE(path string, h gin.HandlerFunc) { g.handle("DELETE", path, h) }
