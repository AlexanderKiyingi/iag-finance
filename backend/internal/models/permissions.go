package models

import "strings"

var builtinRolePermissions = map[string][]string{
	"finance_admin": {
		"dashboard.read", "invoices.read", "invoices.write", "banking.read", "banking.write",
		"assets.read", "assets.write", "approvals.read", "approvals.write", "audit.read", "audit.write",
		"expenses.read", "expenses.write", "workers.read", "users.read", "users.write", "settings.write",
	},
	"finance_manager": {
		"dashboard.read", "invoices.read", "invoices.write", "banking.read", "assets.read",
		"approvals.read", "approvals.write", "audit.read", "expenses.read", "workers.read",
	},
	"finance_viewer": {
		"dashboard.read", "invoices.read", "banking.read", "assets.read", "approvals.read", "audit.read",
	},
}

type PermissionContext struct {
	Role        string   `json:"role"`
	Permissions []string `json:"permissions"`
	CanMutate   bool     `json:"canMutate"`
}

func PermissionContextFor(role string) PermissionContext {
	perms := effectivePermissions(role)
	return PermissionContext{Role: role, Permissions: perms, CanMutate: canMutate(perms)}
}

func (s *Store) PermissionContext() PermissionContext {
	s.mu.RLock()
	role := s.Session.Role
	s.mu.RUnlock()
	return PermissionContextFor(role)
}

func effectivePermissions(role string) []string {
	if p, ok := builtinRolePermissions[role]; ok {
		out := make([]string, len(p))
		copy(out, p)
		return out
	}
	return builtinRolePermissions["finance_viewer"]
}

func canMutate(perms []string) bool {
	for _, p := range perms {
		if strings.HasSuffix(p, ".write") {
			return true
		}
	}
	return false
}

func (s *Store) HasPermission(key string) bool {
	s.mu.RLock()
	role := s.Session.Role
	s.mu.RUnlock()
	for _, p := range effectivePermissions(role) {
		if p == key {
			return true
		}
	}
	return false
}

func (s *Store) RequirePermission(key string) error {
	if !s.HasPermission(key) {
		return ErrForbidden
	}
	return nil
}
