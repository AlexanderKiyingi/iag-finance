package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/iag-finance/backend/internal/authclient"
	"github.com/alvor-technologies/iag-platform-go/apierr"
)

const claimsContextKey = "claims"

// GetClaims returns the verified JWT claims stored on the request context.
func GetClaims(c *gin.Context) (*authclient.Claims, bool) {
	v, ok := c.Get(claimsContextKey)
	if !ok {
		return nil, false
	}
	claims, ok := v.(*authclient.Claims)
	return claims, ok
}

// Principal authenticates HTTP callers via Bearer + JWKS verification only.
// The previous gateway-header mode (X-IAG-* + GATEWAY_INTERNAL_SECRET) has been
// removed; the gateway now forwards Authorization verbatim.
func Principal(verifier *authclient.Verifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		if verifier == nil {
			apierr.Write(c, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "JWT verifier not configured")
			return
		}
		header := c.GetHeader("Authorization")
		if header == "" || !strings.HasPrefix(header, "Bearer ") {
			apierr.Unauthorized(c, "missing bearer token")
			return
		}
		token := strings.TrimPrefix(header, "Bearer ")
		claims, userID, err := verifier.Verify(token)
		if err != nil {
			apierr.Unauthorized(c, "invalid or expired token")
			return
		}
		c.Set("userID", userID)
		c.Set(claimsContextKey, claims)
		c.Next()
	}
}
