package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/iag/finance-backend/internal/models"
)

type Claims struct {
	Email       string `json:"email"`
	Role        string `json:"role"`
	DisplayName string `json:"displayName"`
	Entity      string `json:"entity"`
	jwt.RegisteredClaims
}

type Service struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

func NewService(secret string, accessTTL, refreshTTL time.Duration) *Service {
	return &Service{secret: []byte(secret), accessTTL: accessTTL, refreshTTL: refreshTTL}
}

func (s *Service) Issue(sess models.Session) (models.AuthTokens, string, error) {
	now := time.Now()
	accessClaims := Claims{
		Email: sess.Email, Role: sess.Role, DisplayName: sess.DisplayName, Entity: sess.Entity,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: sess.Email, IssuedAt: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.accessTTL)),
		},
	}
	access, err := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims).SignedString(s.secret)
	if err != nil {
		return models.AuthTokens{}, "", err
	}
	refreshID, err := newTokenID()
	if err != nil {
		return models.AuthTokens{}, "", err
	}
	refreshClaims := jwt.RegisteredClaims{
		ID: refreshID, Subject: sess.Email,
		IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(s.refreshTTL)),
	}
	refresh, err := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims).SignedString(s.secret)
	if err != nil {
		return models.AuthTokens{}, "", err
	}
	return models.AuthTokens{
		AccessToken: access, RefreshToken: refresh, ExpiresIn: int64(s.accessTTL.Seconds()),
	}, refreshID, nil
}

func (s *Service) ParseAccessClaims(token string) (Claims, error) {
	parsed, err := jwt.ParseWithClaims(token, &Claims{}, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return s.secret, nil
	})
	if err != nil {
		return Claims{}, err
	}
	claims, ok := parsed.Claims.(*Claims)
	if !ok || !parsed.Valid {
		return Claims{}, fmt.Errorf("invalid token")
	}
	return *claims, nil
}

func (s *Service) SessionFromClaims(c Claims) models.Session {
	return models.Session{Email: c.Email, Role: c.Role, DisplayName: c.DisplayName, Entity: c.Entity, UserID: c.Email}
}

func (s *Service) ParseRefresh(token string) (tokenID, email string, err error) {
	parsed, err := jwt.ParseWithClaims(token, &jwt.RegisteredClaims{}, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return s.secret, nil
	})
	if err != nil {
		return "", "", err
	}
	claims, ok := parsed.Claims.(*jwt.RegisteredClaims)
	if !ok || !parsed.Valid || claims.ID == "" {
		return "", "", fmt.Errorf("invalid refresh token")
	}
	return claims.ID, claims.Subject, nil
}

func newTokenID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
