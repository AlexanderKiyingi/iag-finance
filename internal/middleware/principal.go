package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/iag-finance/backend/internal/authclient"
	"github.com/iag-finance/backend/internal/config"
)

const claimsContextKey = "claims"

const (
	HeaderUserID        = "X-IAG-User-Id"
	HeaderEmail         = "X-IAG-Email"
	HeaderGroups        = "X-IAG-Groups"
	HeaderRoles         = "X-IAG-Roles"
	HeaderPermissions   = "X-IAG-Permissions"
	HeaderIsSuperuser   = "X-IAG-Is-Superuser"
	HeaderIsStaff       = "X-IAG-Is-Staff"
	HeaderGatewaySecret = "X-IAG-Gateway-Secret"
)

func GetClaims(c *gin.Context) (*authclient.Claims, bool) {
	v, ok := c.Get(claimsContextKey)
	if !ok {
		return nil, false
	}
	claims, ok := v.(*authclient.Claims)
	return claims, ok
}

func Principal(cfg config.Config, verifier *authclient.Verifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		switch cfg.AuthMode {
		case "gateway":
			principalFromGateway(c, cfg.GatewaySecret)
		default:
			principalFromJWT(c, verifier)
		}
	}
}

func principalFromGateway(c *gin.Context, gatewaySecret string) {
	if gatewaySecret != "" && c.GetHeader(HeaderGatewaySecret) != gatewaySecret {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	sub := c.GetHeader(HeaderUserID)
	if sub == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing gateway principal"})
		return
	}

	userID, err := uuid.Parse(sub)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid user id"})
		return
	}

	groups := splitHeaderList(c.GetHeader(HeaderGroups))
	if len(groups) == 0 {
		groups = splitHeaderList(c.GetHeader(HeaderRoles))
	}

	claims := &authclient.Claims{
		Email:       c.GetHeader(HeaderEmail),
		IsSuperuser: strings.EqualFold(c.GetHeader(HeaderIsSuperuser), "true"),
		IsStaff:     strings.EqualFold(c.GetHeader(HeaderIsStaff), "true"),
		Groups:      groups,
		Roles:       groups,
		Permissions: splitHeaderList(c.GetHeader(HeaderPermissions)),
	}
	claims.Subject = sub

	c.Set("userID", userID)
	c.Set(claimsContextKey, claims)
	c.Next()
}

func principalFromJWT(c *gin.Context, verifier *authclient.Verifier) {
	if verifier == nil {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "jwt verifier not configured"})
		return
	}
	header := c.GetHeader("Authorization")
	if header == "" || !strings.HasPrefix(header, "Bearer ") {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing bearer token"})
		return
	}
	token := strings.TrimPrefix(header, "Bearer ")
	claims, userID, err := verifier.Verify(token)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}
	c.Set("userID", userID)
	c.Set(claimsContextKey, claims)
	c.Next()
}

func splitHeaderList(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}
