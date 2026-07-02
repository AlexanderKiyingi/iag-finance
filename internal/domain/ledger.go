package domain

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type AccountType string

const (
	AccountAsset     AccountType = "asset"
	AccountLiability AccountType = "liability"
	AccountEquity    AccountType = "equity"
	AccountRevenue   AccountType = "revenue"
	AccountExpense   AccountType = "expense"
)

type ChartAccount struct {
	ID          uuid.UUID  `json:"id"`
	Code        string     `json:"code"`
	Name        string     `json:"name"`
	AccountType string     `json:"accountType"`
	ParentID    *uuid.UUID `json:"parentId,omitempty"`
	Currency    string     `json:"currency"`
	Active      bool       `json:"active"`
	// RestrictToNaturalSide, when true, rejects a posting to this account's
	// non-natural side (asset/expense may only be debited; liability/equity/
	// revenue may only be credited). Off by default.
	RestrictToNaturalSide bool      `json:"restrictToNaturalSide"`
	CreatedAt             time.Time `json:"createdAt"`
	UpdatedAt             time.Time `json:"updatedAt"`
}

type JournalLine struct {
	ID             uuid.UUID       `json:"id"`
	JournalEntryID uuid.UUID       `json:"journalEntryId"`
	AccountID      uuid.UUID       `json:"accountId"`
	AccountCode    string          `json:"accountCode,omitempty"`
	AccountName    string          `json:"accountName,omitempty"`
	Debit          decimal.Decimal `json:"debit"`
	Credit         decimal.Decimal `json:"credit"`
	Memo           string          `json:"memo"`
	LineOrder      int             `json:"lineOrder"`
}

type JournalEntry struct {
	ID            uuid.UUID `json:"id"`
	EntryNumber   string    `json:"entryNumber"`
	Description   string    `json:"description"`
	Status        string    `json:"status"`
	SourceEventID *string   `json:"sourceEventId,omitempty"`
	SourceService *string   `json:"sourceService,omitempty"`
	CorrelationID *string   `json:"correlationId,omitempty"`
	// AccountingDate is the fiscal date the entry is booked to (period control),
	// distinct from PostedAt (wall-clock). ReversesEntryID points to the original
	// entry when this entry is a reversal.
	AccountingDate  string        `json:"accountingDate,omitempty"`
	ReversesEntryID *uuid.UUID    `json:"reversesEntryId,omitempty"`
	PostedAt        *time.Time    `json:"postedAt,omitempty"`
	CreatedBy       *uuid.UUID    `json:"createdBy,omitempty"`
	CreatedAt       time.Time     `json:"createdAt"`
	UpdatedAt       time.Time     `json:"updatedAt"`
	Lines           []JournalLine `json:"lines,omitempty"`
}

type AROpenItem struct {
	ID             uuid.UUID  `json:"id"`
	CustomerRef    string     `json:"customerRef"`
	DocumentRef    string     `json:"documentRef"`
	Description    string     `json:"description"`
	Amount         string     `json:"amount"`
	AmountPaid     string     `json:"amountPaid"`
	Currency       string     `json:"currency"`
	DueDate        *time.Time `json:"dueDate,omitempty"`
	Status         string     `json:"status"`
	JournalEntryID *uuid.UUID `json:"journalEntryId,omitempty"`
	SourceEventID  *string    `json:"sourceEventId,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type APOpenItem struct {
	ID             uuid.UUID  `json:"id"`
	VendorRef      string     `json:"vendorRef"`
	DocumentRef    string     `json:"documentRef"`
	Description    string     `json:"description"`
	Amount         string     `json:"amount"`
	AmountPaid     string     `json:"amountPaid"`
	Currency       string     `json:"currency"`
	DueDate        *time.Time `json:"dueDate,omitempty"`
	Status         string     `json:"status"`
	JournalEntryID *uuid.UUID `json:"journalEntryId,omitempty"`
	SourceEventID  *string    `json:"sourceEventId,omitempty"`
	PartyID        *uuid.UUID `json:"partyId,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type Payment struct {
	ID             uuid.UUID  `json:"id"`
	Direction      string     `json:"direction"`
	OpenItemID     uuid.UUID  `json:"openItemId"`
	Amount         string     `json:"amount"`
	Currency       string     `json:"currency"`
	PaymentRef     string     `json:"paymentRef"`
	JournalEntryID *uuid.UUID `json:"journalEntryId,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
}

type Adjustment struct {
	ID                  uuid.UUID  `json:"id"`
	Kind                string     `json:"kind"`
	Direction           string     `json:"direction"`
	OriginalDocumentRef string     `json:"originalDocumentRef"`
	DocumentRef         string     `json:"documentRef"`
	PartyRef            string     `json:"partyRef"`
	Amount              string     `json:"amount"`
	Currency            string     `json:"currency"`
	Reason              string     `json:"reason"`
	Status              string     `json:"status"`
	JournalEntryID      *uuid.UUID `json:"journalEntryId,omitempty"`
	CreatedAt           time.Time  `json:"createdAt"`
}
