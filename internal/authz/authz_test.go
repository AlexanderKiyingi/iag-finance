package authz

import "testing"

func TestCanAccessAdmin(t *testing.T) {
	t.Parallel()

	if !CanAccessAdmin(Principal{IsSuperuser: true}) {
		t.Fatal("superuser expected")
	}
	if CanAccessAdmin(Principal{Groups: []string{"user"}, Permissions: []string{"accounts.view_ledger"}}) {
		t.Fatal("regular user should not access admin")
	}
	if !CanAccessAdmin(Principal{IsStaff: true, Groups: []string{"admin"}}) {
		t.Fatal("admin group expected")
	}
}
