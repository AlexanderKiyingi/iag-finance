package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/iag/finance-backend/internal/auth"
	"github.com/iag/finance-backend/internal/config"
	"github.com/iag/finance-backend/internal/models"
)

func GinJWTAuth(cfg config.Config, jwtSvc *auth.Service, store *models.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !cfg.JWTRequired || jwtSvc == nil {
			sess := store.GetSession()
			c.Request = c.Request.WithContext(WithSession(c.Request.Context(), sess))
			c.Next()
			return
		}
		if isPublicRoute(c.Request.Method, c.Request.URL.Path, cfg) {
			c.Next()
			return
		}
		token := bearerToken(c.GetHeader("Authorization"))
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		claims, err := jwtSvc.ParseAccessClaims(token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		sess := jwtSvc.SessionFromClaims(claims)
		store.SetSession(sess)
		c.Request = c.Request.WithContext(WithSession(c.Request.Context(), sess))
		c.Next()
	}
}

func isPublicRoute(method, path string, cfg config.Config) bool {
	path = strings.TrimSuffix(path, "/")
	if strings.HasSuffix(path, "/health") || strings.HasSuffix(path, "/ready") {
		return true
	}
	switch {
	case strings.HasSuffix(path, "/auth/login") && method == http.MethodPost:
		return true
	case strings.HasSuffix(path, "/auth/refresh") && method == http.MethodPost:
		return true
	case strings.HasSuffix(path, "/auth/accounts") && method == http.MethodGet:
		return !cfg.IsProduction
	case strings.HasSuffix(path, "/bootstrap") && method == http.MethodGet:
		return true
	case strings.HasSuffix(path, "/demo/reset") && method == http.MethodPost:
		return cfg.AllowDemoReset
	}
	return false
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}
