package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/iag-finance/backend/internal/authz"
	"github.com/alvor-technologies/iag-platform-go/apierr"
)

// RequireAdmin enforces Django-style admin access (defense in depth when not using the gateway).
func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, ok := GetClaims(c)
		if !ok || claims == nil {
			apierr.Unauthorized(c, "authentication required")
			return
		}
		p := authz.Principal{
			IsSuperuser: claims.IsSuperuser,
			IsStaff:     claims.IsStaff,
			Groups:      claims.Groups,
			Permissions: claims.Permissions,
		}
		if !authz.CanAccessAdmin(p) {
			apierr.WriteWith(c, http.StatusForbidden, apierr.CodeForbidden, "admin access required", gin.H{"require_admin": true})
			return
		}
		c.Next()
	}
}
