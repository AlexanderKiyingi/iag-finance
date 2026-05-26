// Package authclient wraps platform-go's JWKS verifier so existing finance
// handlers keep their (*Claims, uuid.UUID) return surface.
package authclient

import (
	"context"
	"time"

	"github.com/google/uuid"

	platformauthclient "github.com/alvor-technologies/iag-platform-go/authclient"
)

// Claims aliases the platform Claims type.
type Claims = platformauthclient.Claims

// Verifier wraps the platform verifier.
type Verifier struct {
	inner *platformauthclient.Verifier
}

// NewVerifier constructs a Verifier that enforces audience.
func NewVerifier(jwksURL, issuer, audience string) *Verifier {
	return &Verifier{
		inner: platformauthclient.NewVerifier(platformauthclient.Options{
			JWKSURL:  jwksURL,
			Issuer:   issuer,
			Audience: audience,
		}),
	}
}

// Refresh fetches the JWKS.
func (v *Verifier) Refresh(ctx context.Context) error { return v.inner.Refresh(ctx) }

// StartRefreshLoop refreshes on interval until ctx is cancelled.
func (v *Verifier) StartRefreshLoop(ctx context.Context, interval time.Duration) {
	v.inner.StartRefreshLoop(ctx, interval)
}

// Verify validates a Bearer token; returns claims plus user UUID (zero for service principals).
func (v *Verifier) Verify(token string) (*Claims, uuid.UUID, error) {
	claims, err := v.inner.Verify(token)
	if err != nil {
		return nil, uuid.Nil, err
	}
	var uid uuid.UUID
	if claims.IsUser() {
		uid, _ = uuid.Parse(claims.Subject)
	}
	return claims, uid, nil
}
