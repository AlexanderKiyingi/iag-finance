package models

// PermissionDescriptor is posted to iag-authentication at startup.
type PermissionDescriptor struct {
	Name        string
	Description string
}

// PermissionDescriptors returns finance RBAC codenames (gateway-aligned).
func PermissionDescriptors() []PermissionDescriptor {
	return []PermissionDescriptor{
		{Name: "finance.view_ledger", Description: "View chart of accounts, journal entries, and GL reports"},
		{Name: "finance.change_ledger", Description: "Create and post journal entries and chart accounts"},
		{Name: "finance.view_operations", Description: "View finance ops audit, inboxes, and prototype table rows"},
		{Name: "finance.change_operations", Description: "Append finance ops audit events and table rows"},
		{Name: "finance.view_own_ap", Description: "View own accounts-payable lines (vendor portal)"},
	}
}
