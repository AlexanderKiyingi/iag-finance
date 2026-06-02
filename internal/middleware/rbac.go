package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/iag-finance/backend/internal/authz"
)

func principalFromContext(c *gin.Context) (authz.Principal, bool) {
	claims, ok := GetClaims(c)
	if !ok || claims == nil {
		return authz.Principal{}, false
	}
	return authz.Principal{
		IsSuperuser: claims.IsSuperuser,
		IsStaff:     claims.IsStaff,
		Groups:      claims.Groups,
		Permissions: claims.Permissions,
	}, true
}

func requireAnyPermission(perms ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, ok := principalFromContext(c)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		if !authz.HasAnyPermission(p, perms...) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden", "permissions": perms})
			return
		}
		c.Next()
	}
}

// RequireLedgerRead allows GL read APIs (gateway: finance.view_ledger or legacy view_operations).
func RequireLedgerRead() gin.HandlerFunc {
	return requireAnyPermission("finance.view_ledger", "finance.view_operations")
}

// RequireLedgerWrite allows GL mutating APIs.
func RequireLedgerWrite() gin.HandlerFunc {
	return requireAnyPermission("finance.change_ledger", "finance.change_operations")
}

// RequireOperationsRead allows ops audit / prototype table reads.
func RequireOperationsRead() gin.HandlerFunc {
	return requireAnyPermission("finance.view_operations", "finance.view_ledger")
}

// RequireOperationsWrite allows ops audit / table append.
func RequireOperationsWrite() gin.HandlerFunc {
	return requireAnyPermission("finance.change_operations", "finance.change_ledger")
}
