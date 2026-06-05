package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/iag-finance/backend/internal/authz"
	"github.com/alvor-technologies/iag-platform-go/apierr"
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
			apierr.Unauthorized(c, "authentication required")
			return
		}
		if !authz.HasAnyPermission(p, perms...) {
			apierr.WriteWith(c, http.StatusForbidden, apierr.CodeForbidden, "permission denied", gin.H{"required_permission": perms})
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

// RequirePortalAP allows vendor portal users to read their own AP lines.
func RequirePortalAP() gin.HandlerFunc {
	return requireAnyPermission("finance.view_own_ap", "finance.view_own_payment")
}

// RequireOperationsRead allows ops audit / prototype table reads.
func RequireOperationsRead() gin.HandlerFunc {
	return requireAnyPermission("finance.view_operations", "finance.view_ledger")
}

// RequireOperationsWrite allows ops audit / table append.
func RequireOperationsWrite() gin.HandlerFunc {
	return requireAnyPermission("finance.change_operations", "finance.change_ledger")
}
