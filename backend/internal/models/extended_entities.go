package models

type Expense struct {
	ID       string  `json:"id"`
	Date     string  `json:"date"`
	Vendor   string  `json:"vendor"`
	Category string  `json:"category"`
	Amount   float64 `json:"amount"`
	Status   string  `json:"status"`
}

type Worker struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Role   string `json:"role"`
	Dept   string `json:"dept"`
	Status string `json:"status"`
}

type FinanceUser struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Role     string `json:"role"`
	Status   string `json:"status"`
	Billable string `json:"billable"`
}

type Notification struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	Time    string `json:"time"`
	Read    bool   `json:"read"`
}

type Budget struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Period  string  `json:"period"`
	Amount  float64 `json:"amount"`
	Spent   float64 `json:"spent"`
}

type JournalEntry struct {
	ID     string  `json:"id"`
	Date   string  `json:"date"`
	Memo   string  `json:"memo"`
	Debit  float64 `json:"debit"`
	Credit float64 `json:"credit"`
	Status string  `json:"status"`
}

type InventoryItem struct {
	Code     string  `json:"code"`
	Name     string  `json:"name"`
	Category string  `json:"category"`
	Qty      float64 `json:"qty"`
	Rate     float64 `json:"rate"`
}

type TaxRecord struct {
	ID     string  `json:"id"`
	Period string  `json:"period"`
	Type   string  `json:"type"`
	Amount float64 `json:"amount"`
	Status string  `json:"status"`
}
