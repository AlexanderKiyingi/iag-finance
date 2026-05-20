package authz

import "slices"

// Principal mirrors Django auth state from the authentication service JWT / gateway headers.
type Principal struct {
	IsSuperuser bool
	IsStaff     bool
	Groups      []string
	Permissions []string
}

func IsSuperuser(p Principal) bool {
	if p.IsSuperuser {
		return true
	}
	return slices.Contains(p.Groups, "superadmin")
}

func IsStaff(p Principal) bool {
	if IsSuperuser(p) {
		return true
	}
	if p.IsStaff {
		return true
	}
	return slices.Contains(p.Groups, "admin") || slices.Contains(p.Groups, "staff")
}

func HasAnyPermission(p Principal, required ...string) bool {
	if IsSuperuser(p) {
		return true
	}
	for _, codename := range required {
		if slices.Contains(p.Permissions, codename) {
			return true
		}
	}
	return false
}

// CanAccessAdmin matches gateway requireAdmin and Django admin site access.
func CanAccessAdmin(p Principal) bool {
	if IsSuperuser(p) {
		return true
	}
	if !IsStaff(p) {
		return false
	}
	if slices.Contains(p.Groups, "admin") {
		return true
	}
	return HasAnyPermission(p,
		"auth.change_user",
		"auth.change_group",
	)
}
