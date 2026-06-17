package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/alvor-technologies/iag-platform-go/apierr"
	"github.com/iag-finance/backend/internal/repository"
)

// EntityContext resolves the working accounting entity from the X-Entity-Id
// header (a UUID) and stores it in the request context, so writes are stamped
// with it and entity-aware reads scope to it. Absent → the default entity.
//
// NOTE: this is a scoping mechanism, not yet a per-user authorization boundary —
// any caller may select any entity. Restricting which entities a user may access
// requires an auth-service claim (e.g. an `entities` list in the JWT); wire that
// in here when available.
func EntityContext() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := c.GetHeader("X-Entity-Id")
		if raw == "" {
			c.Next()
			return
		}
		id, err := uuid.Parse(raw)
		if err != nil {
			apierr.JSONStatus(c, 400, "X-Entity-Id must be a valid UUID")
			c.Abort()
			return
		}
		c.Request = c.Request.WithContext(repository.WithEntity(c.Request.Context(), id))
		c.Set("entityID", id)
		c.Next()
	}
}
