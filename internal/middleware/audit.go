package middleware

import (
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
			id := raw.(uuid.UUID)
			actorID = &id
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
		_ = svc.Record(c.Request.Context(), auditlog.RecordInput{
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
		})
	}
}
