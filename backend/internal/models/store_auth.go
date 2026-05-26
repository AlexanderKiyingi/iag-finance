package models

import (
	"context"
	"strings"

	"github.com/iag/finance-backend/internal/security"
)

var memoryAuthAccounts = BuiltinAuthAccounts()

func (s *Store) ListAuthAccounts() []AuthAccount {
	if s.repo != nil {
		list, err := s.repo.ListAuthAccounts(context.Background())
		if err == nil && len(list) > 0 {
			return list
		}
	}
	out := make([]AuthAccount, len(memoryAuthAccounts))
	copy(out, memoryAuthAccounts)
	for i := range out {
		out[i].Password = ""
	}
	return out
}

func (s *Store) LoginWithTokens(email, password string) (Session, *AuthTokens, error) {
	sess, err := s.authenticate(email, password)
	if err != nil {
		return Session{}, nil, err
	}
	s.SetSession(sess)
	var tokens *AuthTokens
	if s.jwt != nil {
		pair, refreshID, err := s.jwt.Issue(sess)
		if err != nil {
			return Session{}, nil, err
		}
		tokens = &pair
		if s.tokens != nil {
			_ = s.tokens.SaveRefresh(context.Background(), refreshID, sess.Email)
		}
	}
	s.appendAudit("Login", sess.Email, sess.DisplayName)
	return sess, tokens, nil
}

func (s *Store) RefreshAccess(refreshToken string) (Session, *AuthTokens, error) {
	if s.jwt == nil {
		return Session{}, nil, ErrValidation
	}
	tokenID, email, err := s.jwt.ParseRefresh(refreshToken)
	if err != nil {
		return Session{}, nil, ErrUnauthorized
	}
	if s.tokens != nil {
		stored, err := s.tokens.ConsumeRefresh(context.Background(), tokenID)
		if err != nil || !strings.EqualFold(stored, email) {
			return Session{}, nil, ErrUnauthorized
		}
	}
	sess, err := s.lookupUser(email)
	if err != nil {
		return Session{}, nil, err
	}
	s.SetSession(sess)
	pair, newID, err := s.jwt.Issue(sess)
	if err != nil {
		return Session{}, nil, err
	}
	if s.tokens != nil {
		_ = s.tokens.SaveRefresh(context.Background(), newID, sess.Email)
	}
	return sess, &pair, nil
}

func (s *Store) authenticate(email, password string) (Session, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return Session{}, ErrValidation
	}
	if s.repo != nil {
		acc, err := s.repo.FindAuthAccount(context.Background(), email)
		if err == nil {
			if password == "" || !security.VerifyPassword(acc.Password, password) {
				return Session{}, ErrUnauthorized
			}
			return sessionFromAccount(acc), nil
		}
		if err != ErrNotFound {
			return Session{}, err
		}
	}
	for _, a := range memoryAuthAccounts {
		if strings.EqualFold(a.Email, email) {
			if password != "" && a.Password != password {
				return Session{}, ErrUnauthorized
			}
			return sessionFromAccount(a), nil
		}
	}
	return Session{}, ErrUnauthorized
}

func (s *Store) lookupUser(email string) (Session, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if s.repo != nil {
		acc, err := s.repo.FindAuthAccount(context.Background(), email)
		if err == nil {
			return sessionFromAccount(acc), nil
		}
		if err != ErrNotFound {
			return Session{}, err
		}
	}
	for _, a := range memoryAuthAccounts {
		if strings.EqualFold(a.Email, email) {
			return sessionFromAccount(a), nil
		}
	}
	return Session{}, ErrNotFound
}

func sessionFromAccount(a AuthAccount) Session {
	return Session{
		UserID: a.Email, Email: a.Email, Role: a.Role,
		DisplayName: a.DisplayName, Entity: a.Entity,
	}
}

func (s *Store) appendAudit(action, entity, user string) {
	s.mu.Lock()
	entry := AuditEntry{TS: nowTS(), User: user, Entity: entity, Action: action}
	s.Audit = append([]AuditEntry{entry}, s.Audit...)
	if len(s.Audit) > 200 {
		s.Audit = s.Audit[:200]
	}
	s.mu.Unlock()
	s.afterMutation()
}
