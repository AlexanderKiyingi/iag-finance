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
// Authorization: selecting a NON-default entity requires finance.cross_entity
// (superuser bypasses). This closes the "any caller can pick any entity" hole.
// Per-entity (rather than all-or-nothing) authorization additionally needs an
// auth-service claim listing a user's permitted entities — check it here when
// the token carries one.
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
		if id != repository.DefaultEntityID && !HasPerm(c, "finance.cross_entity") {
			apierr.WriteWith(c, 403, apierr.CodeForbidden, "not permitted to access this entity",
				gin.H{"required_permission": "finance.cross_entity"})
			c.Abort()
			return
		}
		c.Request = c.Request.WithContext(repository.WithEntity(c.Request.Context(), id))
		c.Set("entityID", id)
		c.Next()
	}
}
