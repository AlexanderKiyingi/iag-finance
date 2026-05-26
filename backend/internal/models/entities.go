package models

type Company struct {
	Name    string   `json:"name"`
	Trading string   `json:"trading"`
	Address []string `json:"address"`
	Email   string   `json:"email"`
	VAT     string   `json:"vat"`
	EFRIS   string   `json:"efris"`
}

type Session struct {
	UserID      string `json:"userId"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
	Entity      string `json:"entity"`
}

type Invoice struct {
	No       string  `json:"no"`
	Date     string  `json:"date"`
	Due      string  `json:"due"`
	Customer string  `json:"customer"`
	Total    float64 `json:"total"`
	Balance  float64 `json:"balance"`
	Status   string  `json:"status"`
}

type InvoiceInput struct {
	Date     string  `json:"date"`
	Due      string  `json:"due"`
	Customer string  `json:"customer"`
	Total    float64 `json:"total"`
	Balance  float64 `json:"balance"`
	Status   string  `json:"status"`
}

type InvoicePatch struct {
	Date     *string  `json:"date,omitempty"`
	Due      *string  `json:"due,omitempty"`
	Customer *string  `json:"customer,omitempty"`
	Total    *float64 `json:"total,omitempty"`
	Balance  *float64 `json:"balance,omitempty"`
	Status   *string  `json:"status,omitempty"`
}

type BankAccount struct {
	Name     string  `json:"name"`
	Balance  float64 `json:"balance"`
	InBooks  float64 `json:"inBooks"`
	Review   int     `json:"review"`
	Type     string  `json:"type"`
	Currency string  `json:"currency,omitempty"`
}

type BankTx struct {
	Date      string   `json:"date"`
	Desc      string   `json:"desc"`
	Payee     string   `json:"payee"`
	Category  string   `json:"category"`
	Spent     *float64 `json:"spent"`
	Received  *float64 `json:"received"`
	Action    string   `json:"action"`
	Matched   *string  `json:"matched"`
}

type FixedAsset struct {
	Tag      string  `json:"tag"`
	Name     string  `json:"name"`
	Category string  `json:"category"`
	Acq      string  `json:"acq"`
	Cost     float64 `json:"cost"`
	Useful   int     `json:"useful"`
	Method   string  `json:"method"`
	Residual float64 `json:"residual"`
	AccumDep float64 `json:"accumDep"`
	NBV      float64 `json:"nbv"`
	Location string  `json:"location"`
	Custodian string `json:"custodian"`
	Status   string  `json:"status"`
}

type Approval struct {
	ID      string  `json:"id"`
	Type    string  `json:"type"`
	Subject string  `json:"subject"`
	Status  string  `json:"status"`
	Date    string  `json:"date"`
	Amount  float64 `json:"amount,omitempty"`
}

type AuditEntry struct {
	TS     string `json:"ts"`
	User   string `json:"user"`
	Entity string `json:"entity"`
	Action string `json:"action"`
}

type DashBar struct {
	D         string  `json:"d"`
	Date      string  `json:"date"`
	Sav       float64 `json:"sav"`
	Inc       float64 `json:"inc"`
	Exp       float64 `json:"exp"`
	Highlight bool    `json:"highlight,omitempty"`
}

type DashboardKPI struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Trend string `json:"trend"`
	Up    *bool  `json:"up,omitempty"`
	Href  string `json:"href"`
}

type FinanceUpdate struct {
	Icon   string `json:"icon"`
	Bg     string `json:"bg"`
	Color  string `json:"color"`
	Title  string `json:"title"`
	Desc   string `json:"desc"`
	Time   string `json:"time"`
	Href   string `json:"href"`
	Period string `json:"period"`
}

type DashboardPayload struct {
	KPIs     []DashboardKPI `json:"kpis"`
	CashFlow CashFlowSummary `json:"cashFlow"`
	Updates  []FinanceUpdate `json:"updates"`
	OpenAR   float64         `json:"openAr"`
}

type CashFlowSummary struct {
	TotalLabel string    `json:"totalLabel"`
	Change     string    `json:"change"`
	Periods    []string  `json:"periods"`
	Bars       []DashBar `json:"bars"`
}

type SessionPatch struct {
	DisplayName *string `json:"displayName,omitempty"`
	Entity      *string `json:"entity,omitempty"`
}

type ListMeta struct {
	Total int `json:"total"`
	Page  int `json:"page,omitempty"`
	Limit int `json:"limit,omitempty"`
}

type InvoiceListResponse struct {
	Items []Invoice `json:"items"`
	Meta  ListMeta  `json:"meta"`
}

type AssetSummary struct {
	GrossBook      float64 `json:"grossBook"`
	AccumDep       float64 `json:"accumDep"`
	NetBook        float64 `json:"netBook"`
	MonthlyDep     float64 `json:"monthlyDep"`
	Count          int     `json:"count"`
}

type AssetListResponse struct {
	Items   []FixedAsset `json:"items"`
	Summary AssetSummary `json:"summary"`
	Meta    ListMeta     `json:"meta"`
}
