package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/iag-finance/backend/internal/auditlog"
)

// AuditLog records every HTTP request after the handler completes.
func AuditLog(svc *auditlog.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}

		var actorID *uuid.UUID
		if raw, ok := c.Get("userID"); ok {
			// comma-ok guards against a non-UUID principal id panicking the
			// deferred audit step (and taking the request down via Recovery).
			if id, ok := raw.(uuid.UUID); ok {
				actorID = &id
			}
		}

		email := ""
		if claims, ok := GetClaims(c); ok && claims != nil {
			email = claims.Email
		}

		correlationID := c.GetHeader("X-Correlation-Id")
		if correlationID == "" {
			correlationID = c.GetHeader("X-Request-Id")
		}

		status := c.Writer.Status()
		if err := svc.Record(c.Request.Context(), auditlog.RecordInput{
			EventType:     auditlog.EventHTTPRequest,
			ActorID:       actorID,
			ActorEmail:    email,
			HTTPMethod:    c.Request.Method,
			HTTPPath:      path,
			StatusCode:    status,
			IPAddress:     c.ClientIP(),
			UserAgent:     c.Request.UserAgent(),
			CorrelationID: correlationID,
			Metadata: map[string]any{
				"durationMs": time.Since(start).Milliseconds(),
				"query":      c.Request.URL.RawQuery,
			},
		}); err != nil {
			// An audit write failing is a compliance signal, not something to
			// silently drop — surface it loudly (the request itself stands).
			slog.Error("audit log write failed", "method", c.Request.Method, "path", path, "err", err)
		}
	}
}
