package models

type AuthAccount struct {
	Email       string `json:"email"`
	Password    string `json:"password,omitempty"`
	Role        string `json:"role"`
	DisplayName string `json:"displayName"`
	Entity      string `json:"entity"`
}

type AuthTokens struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresIn    int64  `json:"expiresIn"`
}

type LoginInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type RefreshInput struct {
	RefreshToken string `json:"refreshToken"`
}

type LoginResponse struct {
	Session     Session           `json:"session"`
	Permissions PermissionContext `json:"permissions"`
	Tokens      *AuthTokens       `json:"tokens,omitempty"`
}

type BootstrapResponse struct {
	Session     Session           `json:"session"`
	Company     Company           `json:"company"`
	Invoices    []Invoice         `json:"invoices"`
	BankAccounts []BankAccount    `json:"bankAccounts"`
	BankTx      []BankTx          `json:"bankTransactions"`
	FixedAssets []FixedAsset      `json:"fixedAssets"`
	Approvals   []Approval        `json:"approvals"`
	AuditLog    []AuditEntry      `json:"auditLog"`
	Expenses    []Expense         `json:"expenses"`
	Workers     []Worker          `json:"workers"`
	Users       []FinanceUser     `json:"users"`
	Notifications []Notification  `json:"notifications"`
	Budgets     []Budget          `json:"budgets"`
	Journals    []JournalEntry    `json:"journals"`
	Inventory   []InventoryItem   `json:"inventory"`
	Taxes       []TaxRecord       `json:"taxes"`
	Settings    map[string]string `json:"settings"`
	Permissions PermissionContext `json:"permissions"`
}

func BuiltinAuthAccounts() []AuthAccount {
	return []AuthAccount{
		{Email: "finance@iag.africa", Password: "Finance123!", Role: "finance_admin", DisplayName: "Finance Team", Entity: "Africa Coffee Park"},
		{Email: "kassim@iagcoffee.com", Password: "Cfo123!", Role: "finance_manager", DisplayName: "M. Kassim (CFO)", Entity: "Africa Coffee Park"},
		{Email: "viewer@iag.africa", Password: "Viewer123!", Role: "finance_viewer", DisplayName: "Reports Only", Entity: "Africa Coffee Park"},
	}
}
