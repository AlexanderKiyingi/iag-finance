package handlers

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/iag-finance/backend/internal/auditlog"
	"github.com/iag-finance/backend/internal/middleware"
)

func actorFromContext(c *gin.Context) (*uuid.UUID, string) {
	var actorID *uuid.UUID
	if raw, ok := c.Get("userID"); ok {
		id := raw.(uuid.UUID)
		actorID = &id
	}
	email := ""
	if claims, ok := middleware.GetClaims(c); ok && claims != nil {
		email = claims.Email
	}
	return actorID, email
}

// chainActor derives a stable, non-forgeable actor label for the audit chain
// from the authenticated principal: email when present, else the user id, else
// "anonymous". Never taken from the request body.
func chainActor(c *gin.Context) string {
	actorID, email := actorFromContext(c)
	if email != "" {
		return email
	}
	if actorID != nil {
		return actorID.String()
	}
	return "anonymous"
}

func correlationID(c *gin.Context) string {
	if id := c.GetHeader("X-Correlation-Id"); id != "" {
		return id
	}
	return c.GetHeader("X-Request-Id")
}

func logBusinessEvent(c *gin.Context, svc *auditlog.Service, eventType, resourceType, resourceID string, statusCode int, metadata map[string]any) {
	if svc == nil {
		return
	}
	actorID, email := actorFromContext(c)
	_ = svc.Record(c.Request.Context(), auditlog.RecordInput{
		EventType:     eventType,
		ActorID:       actorID,
		ActorEmail:    email,
		ResourceType:  resourceType,
		ResourceID:    resourceID,
		HTTPMethod:    c.Request.Method,
		HTTPPath:      c.FullPath(),
		StatusCode:    statusCode,
		IPAddress:     c.ClientIP(),
		UserAgent:     c.Request.UserAgent(),
		CorrelationID: correlationID(c),
		Metadata:      metadata,
	})
}
