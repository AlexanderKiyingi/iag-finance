package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/iag-finance/backend/internal/authz"
)

// RequireAdmin enforces Django-style admin access (defense in depth when not using the gateway).
func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, ok := GetClaims(c)
		if !ok || claims == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		groups := claims.Groups
		if len(groups) == 0 {
			groups = claims.Roles
		}
		p := authz.Principal{
			IsSuperuser: claims.IsSuperuser,
			IsStaff:     claims.IsStaff,
			Groups:      groups,
			Permissions: claims.Permissions,
		}
		if !authz.CanAccessAdmin(p) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden", "require_admin": true})
			return
		}
		c.Next()
	}
}
