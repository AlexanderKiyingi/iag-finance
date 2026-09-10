// Package appkv is the app-shell key/value store.
//
// The finance web app calls a family of endpoints that had no server on the
// platform at all — app settings, a shared KV, server-persisted form drafts,
// web-push subscriptions and request-email routing. Each was reachable only
// through a rewrite to the legacy Go API, so a platform-only deployment lost
// all five: settings could not be saved, drafts vanished on reload, and push
// could not be subscribed to.
//
// They are one behaviour — a scoped JSON document under a namespace and a key —
// so they share one table and one set of verbs rather than five of each.
package appkv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a document does not exist.
var ErrNotFound = errors.New("app_kv: not found")

// ErrInvalidScope guards the owner/scope pairing the table's CHECK constraint
// also enforces — caught here so the caller gets a clear message rather than a
// constraint violation surfaced as a 500.
var ErrInvalidScope = errors.New("app_kv: user scope needs an owner, global scope must not have one")

// Scope values.
const (
	ScopeGlobal = "global"
	ScopeUser   = "user"
)

// Doc is one stored document.
type Doc struct {
	Namespace string          `json:"namespace"`
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value"`
	UpdatedAt string          `json:"updatedAt"`
	UpdatedBy string          `json:"updatedBy,omitempty"`
}

type Store struct {
	Pool *pgxpool.Pool
}

// Namespaces this service will serve, and whether each is per-user.
//
// A whitelist rather than free-form: the namespace arrives in the URL, and an
// open store invites callers to keep arbitrary state in the finance database
// that nothing here knows how to migrate, back up or reason about.
var namespaceScope = map[string]string{
	"settings":               ScopeGlobal,
	"request-email-contacts": ScopeGlobal,
	"drafts":                 ScopeUser,
	"push":                   ScopeUser,
	"kv":                     ScopeUser,
}

// ScopeFor reports the scope a namespace is stored under, and whether the
// namespace is served at all.
func ScopeFor(namespace string) (string, bool) {
	scope, ok := namespaceScope[strings.TrimSpace(namespace)]
	return scope, ok
}

// IsGlobal reports whether writes to a namespace are tenant-wide (and so need
// an administrative grant) rather than private to the caller.
func IsGlobal(namespace string) bool {
	scope, ok := ScopeFor(namespace)
	return ok && scope == ScopeGlobal
}

func ownerFor(scope, userID string) (string, error) {
	if scope == ScopeUser {
		if strings.TrimSpace(userID) == "" {
			return "", ErrInvalidScope
		}
		return userID, nil
	}
	return "", nil
}

// Get reads one document.
func (s *Store) Get(ctx context.Context, namespace, key, userID string) (*Doc, error) {
	scope, ok := ScopeFor(namespace)
	if !ok {
		return nil, fmt.Errorf("app_kv: unknown namespace %q", namespace)
	}
	owner, err := ownerFor(scope, userID)
	if err != nil {
		return nil, err
	}

	var doc Doc
	err = s.Pool.QueryRow(ctx, `
		SELECT namespace, key, value, to_char(updated_at, 'YYYY-MM-DD"T"HH24:MI:SSZ'), updated_by
		FROM app_kv
		WHERE scope = $1 AND owner_id = $2 AND namespace = $3 AND key = $4
	`, scope, owner, namespace, key).Scan(
		&doc.Namespace, &doc.Key, &doc.Value, &doc.UpdatedAt, &doc.UpdatedBy,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

// List returns every document in a namespace, for the caller's scope.
func (s *Store) List(ctx context.Context, namespace, userID string) ([]Doc, error) {
	scope, ok := ScopeFor(namespace)
	if !ok {
		return nil, fmt.Errorf("app_kv: unknown namespace %q", namespace)
	}
	owner, err := ownerFor(scope, userID)
	if err != nil {
		return nil, err
	}

	rows, err := s.Pool.Query(ctx, `
		SELECT namespace, key, value, to_char(updated_at, 'YYYY-MM-DD"T"HH24:MI:SSZ'), updated_by
		FROM app_kv
		WHERE scope = $1 AND owner_id = $2 AND namespace = $3
		ORDER BY key ASC
	`, scope, owner, namespace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Doc{}
	for rows.Next() {
		var doc Doc
		if err := rows.Scan(&doc.Namespace, &doc.Key, &doc.Value, &doc.UpdatedAt, &doc.UpdatedBy); err != nil {
			return nil, err
		}
		out = append(out, doc)
	}
	return out, rows.Err()
}

// Put upserts one document and returns the stored row.
func (s *Store) Put(ctx context.Context, namespace, key string, value json.RawMessage, userID, actor string) (*Doc, error) {
	scope, ok := ScopeFor(namespace)
	if !ok {
		return nil, fmt.Errorf("app_kv: unknown namespace %q", namespace)
	}
	owner, err := ownerFor(scope, userID)
	if err != nil {
		return nil, err
	}
	if len(value) == 0 {
		value = json.RawMessage("null")
	}

	var doc Doc
	err = s.Pool.QueryRow(ctx, `
		INSERT INTO app_kv (scope, owner_id, namespace, key, value, updated_at, updated_by)
		VALUES ($1, $2, $3, $4, $5, NOW(), $6)
		ON CONFLICT (scope, owner_id, namespace, key) DO UPDATE SET
			value = EXCLUDED.value,
			updated_at = NOW(),
			updated_by = EXCLUDED.updated_by
		RETURNING namespace, key, value, to_char(updated_at, 'YYYY-MM-DD"T"HH24:MI:SSZ'), updated_by
	`, scope, owner, namespace, key, value, actor).Scan(
		&doc.Namespace, &doc.Key, &doc.Value, &doc.UpdatedAt, &doc.UpdatedBy,
	)
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

// Delete removes one document. Deleting something that is not there is not an
// error: the caller's goal state (no such document) already holds.
func (s *Store) Delete(ctx context.Context, namespace, key, userID string) error {
	scope, ok := ScopeFor(namespace)
	if !ok {
		return fmt.Errorf("app_kv: unknown namespace %q", namespace)
	}
	owner, err := ownerFor(scope, userID)
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `
		DELETE FROM app_kv
		WHERE scope = $1 AND owner_id = $2 AND namespace = $3 AND key = $4
	`, scope, owner, namespace, key)
	return err
}
