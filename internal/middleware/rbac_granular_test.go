package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/iag-finance/backend/internal/authclient"
	"github.com/iag-finance/backend/internal/repository"
)

func runWithClaims(t *testing.T, claims *authclient.Claims, gate gin.HandlerFunc, req *http.Request) int {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/x", func(c *gin.Context) {
		if claims != nil {
			c.Set(claimsContextKey, claims)
		}
	}, gate, func(c *gin.Context) { c.Status(http.StatusOK) })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

// A granular gate accepts its specific permission OR the broad change_ledger OR
// superuser, and denies a view-only user.
func TestRequireGranular(t *testing.T) {
	gate := Require("finance.close_period", "finance.change_ledger")
	get := httptest.NewRequest(http.MethodGet, "/x", nil)

	cases := []struct {
		name   string
		claims *authclient.Claims
		want   int
	}{
		{"specific perm", &authclient.Claims{Permissions: []string{"finance.close_period"}}, http.StatusOK},
		{"baseline change_ledger", &authclient.Claims{Permissions: []string{"finance.change_ledger"}}, http.StatusOK},
		{"superuser", &authclient.Claims{IsSuperuser: true}, http.StatusOK},
		{"view-only denied", &authclient.Claims{Permissions: []string{"finance.view_ledger"}}, http.StatusForbidden},
		{"unauthenticated", nil, http.StatusUnauthorized},
	}
	for _, tc := range cases {
		if got := runWithClaims(t, tc.claims, gate, get); got != tc.want {
			t.Errorf("%s: status = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// Selecting a non-default entity via X-Entity-Id requires finance.cross_entity;
// the default entity needs nothing; an invalid header is a 400.
func TestEntityContextAuthorization(t *testing.T) {
	gate := EntityContext()
	other := uuid.New().String()

	withEntity := func(entity string) *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		if entity != "" {
			req.Header.Set("X-Entity-Id", entity)
		}
		return req
	}

	cases := []struct {
		name   string
		claims *authclient.Claims
		req    *http.Request
		want   int
	}{
		{"no header → ok", &authclient.Claims{Permissions: []string{"finance.view_ledger"}}, withEntity(""), http.StatusOK},
		{"default entity → ok without perm", &authclient.Claims{Permissions: []string{"finance.view_ledger"}}, withEntity(repository.DefaultEntityID.String()), http.StatusOK},
		{"other entity denied without perm", &authclient.Claims{Permissions: []string{"finance.view_ledger"}}, withEntity(other), http.StatusForbidden},
		{"other entity allowed with cross_entity", &authclient.Claims{Permissions: []string{"finance.cross_entity"}}, withEntity(other), http.StatusOK},
		{"other entity allowed for superuser", &authclient.Claims{IsSuperuser: true}, withEntity(other), http.StatusOK},
		{"invalid header → 400", &authclient.Claims{IsSuperuser: true}, withEntity("not-a-uuid"), http.StatusBadRequest},
	}
	for _, tc := range cases {
		if got := runWithClaims(t, tc.claims, gate, tc.req); got != tc.want {
			t.Errorf("%s: status = %d, want %d", tc.name, got, tc.want)
		}
	}
}
