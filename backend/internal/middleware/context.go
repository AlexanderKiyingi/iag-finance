package middleware

import (
	"context"

	"github.com/iag/finance-backend/internal/models"
)

type sessionKey struct{}

func WithSession(ctx context.Context, sess models.Session) context.Context {
	return context.WithValue(ctx, sessionKey{}, sess)
}

func SessionFromContext(ctx context.Context) (models.Session, bool) {
	s, ok := ctx.Value(sessionKey{}).(models.Session)
	return s, ok
}
