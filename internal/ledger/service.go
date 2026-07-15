package ledger

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/domain"
	"github.com/iag-finance/backend/internal/repository"
)

var (
	ErrUnbalancedEntry = errors.New("journal entry is not balanced")
	ErrEmptyEntry      = errors.New("journal entry has no lines")
	ErrInvalidStatus   = errors.New("invalid journal status transition")
	ErrDuplicateEvent  = errors.New("source event already processed")
	ErrAccountNotFound = errors.New("account not found")
	ErrPeriodClosed    = errors.New("accounting period is closed")
	ErrUnnaturalSide   = errors.New("account restricted to its natural balance side")

	// Re-exported so handlers map repository reversal errors without importing
	// the repository package directly.
	ErrNotReversible = repository.ErrNotReversible
	ErrEntryNotFound = repository.ErrEntryNotFound
	ErrNotDraft      = repository.ErrNotDraft
)

type LineInput struct {
	AccountCode  string
	Debit        decimal.Decimal
	Credit       decimal.Decimal
	Memo         string
	CostCenterID *uuid.UUID
	ProjectID    *uuid.UUID
}

type CreateEntryInput struct {
	Description          string
	Lines                []LineInput
	SourceEventID        *string
	SourceService        *string
	CorrelationID        *string
	CreatedBy            *uuid.UUID
	AccountingDate       *time.Time
	CounterpartyEntityID *uuid.UUID
}

type Service struct {
	repo *repository.Repository
}

func New(repo *repository.Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Seed(ctx context.Context) error {
	return s.repo.SeedChartOfAccounts(ctx)
}

func (s *Service) ListChartOfAccounts(ctx context.Context) ([]domain.ChartAccount, error) {
	return s.repo.ListChartOfAccounts(ctx)
}

func (s *Service) CreateChartAccount(ctx context.Context, code, name, accountType, currency string, parentID *uuid.UUID) (*domain.ChartAccount, error) {
	return s.repo.CreateChartAccount(ctx, code, name, accountType, currency, parentID)
}

func (s *Service) UpdateChartAccount(ctx context.Context, id uuid.UUID, name, accountType, currency *string, parentID *uuid.UUID, active, restrictNaturalSide *bool) (*domain.ChartAccount, error) {
	return s.repo.UpdateChartAccount(ctx, id, name, accountType, currency, parentID, active, restrictNaturalSide)
}

func (s *Service) DeactivateChartAccount(ctx context.Context, id uuid.UUID) (bool, error) {
	return s.repo.DeactivateChartAccount(ctx, id)
}

func (s *Service) ListJournalEntries(ctx context.Context, limit, offset int) ([]domain.JournalEntry, error) {
	return s.repo.ListJournalEntries(ctx, limit, offset)
}

func (s *Service) GetJournalEntry(ctx context.Context, id uuid.UUID) (*domain.JournalEntry, error) {
	return s.repo.GetJournalEntry(ctx, id)
}

// DeleteDraftJournalEntry discards an unposted draft entry (posted entries must
// be reversed). Returns ErrEntryNotFound / ErrNotDraft.
func (s *Service) DeleteDraftJournalEntry(ctx context.Context, id uuid.UUID) error {
	return s.repo.DeleteDraftEntry(ctx, id)
}

// DeactivateCostCenter soft-archives a cost-centre dimension.
func (s *Service) DeactivateCostCenter(ctx context.Context, id uuid.UUID) error {
	return s.repo.DeactivateCostCenter(ctx, id)
}

func (s *Service) ListARItems(ctx context.Context, limit, offset int) ([]domain.AROpenItem, error) {
	return s.repo.ListAROpenItems(ctx, limit, offset)
}

func (s *Service) ListAPItems(ctx context.Context, limit, offset int) ([]domain.APOpenItem, error) {
	return s.repo.ListAPOpenItems(ctx, limit, offset)
}

func (s *Service) ListAPByPartyID(ctx context.Context, partyID uuid.UUID, limit, offset int) ([]domain.APOpenItem, error) {
	return s.repo.ListAPByPartyID(ctx, partyID, limit, offset)
}

func (s *Service) PartyIDForPlatformUser(ctx context.Context, platformUserID uuid.UUID) (uuid.UUID, error) {
	return s.repo.PartyIDForPlatformUser(ctx, platformUserID)
}

func (s *Service) CreateARItem(ctx context.Context, customerRef, documentRef, description, amount, currency string, dueDate *time.Time, outbox *repository.OutboxEvent) (*domain.AROpenItem, error) {
	return s.repo.CreateAROpenItem(ctx, customerRef, documentRef, description, amount, currency, dueDate, nil, nil, outbox)
}

func (s *Service) CreateARItemWithBilling(ctx context.Context, customerRef, documentRef, description, amount, currency string, dueDate *time.Time, billingOrgID, billingIdentityID *uuid.UUID, outbox *repository.OutboxEvent) (*domain.AROpenItem, error) {
	return s.repo.CreateAROpenItemWithBilling(ctx, customerRef, documentRef, description, amount, currency, dueDate, billingOrgID, billingIdentityID, outbox)
}

func (s *Service) GetAROpenItemByID(ctx context.Context, id uuid.UUID) (*domain.AROpenItem, error) {
	return s.repo.GetAROpenItem(ctx, id)
}

func (s *Service) CreateAPItem(ctx context.Context, vendorRef, documentRef, description, amount, currency string, dueDate *time.Time, outbox *repository.OutboxEvent) (*domain.APOpenItem, error) {
	return s.repo.CreateAPOpenItem(ctx, vendorRef, documentRef, description, amount, currency, dueDate, nil, nil, outbox)
}

// LinkARToJournal attaches a posted journal entry to an AR open item by document_ref.
func (s *Service) LinkARToJournal(ctx context.Context, documentRef string, journalEntryID uuid.UUID, sourceEventID string) error {
	if documentRef == "" {
		return nil
	}
	return s.repo.LinkAROpenItemByDocumentRef(ctx, documentRef, journalEntryID, sourceEventID)
}

// LinkAPToJournal attaches a posted journal entry to an AP open item by document_ref.
func (s *Service) LinkAPToJournal(ctx context.Context, documentRef string, journalEntryID uuid.UUID, sourceEventID string) error {
	if documentRef == "" {
		return nil
	}
	return s.repo.LinkAPOpenItemByDocumentRef(ctx, documentRef, journalEntryID, sourceEventID)
}

func (s *Service) TrialBalance(ctx context.Context, from, to *time.Time, entityIDs []uuid.UUID) ([]repository.TrialBalanceRow, error) {
	return s.repo.TrialBalance(ctx, from, to, entityIDs)
}

func (s *Service) ControlReconciliation(ctx context.Context, entityIDs []uuid.UUID) ([]repository.ControlReconRow, error) {
	return s.repo.ControlReconciliation(ctx, entityIDs)
}

func (s *Service) ARAging(ctx context.Context) ([]repository.ARAgingBucket, error) {
	return s.repo.ARAging(ctx)
}

func (s *Service) APAging(ctx context.Context) ([]repository.ARAgingBucket, error) {
	return s.repo.APAging(ctx)
}

func (s *Service) GLAccountDetail(ctx context.Context, code string, from, to *time.Time) ([]repository.GLAccountLine, error) {
	return s.repo.GLAccountDetail(ctx, code, from, to)
}

func (s *Service) ProfitAndLoss(ctx context.Context, from, to *time.Time, entityIDs []uuid.UUID) ([]repository.PLRow, error) {
	return s.repo.ProfitAndLoss(ctx, from, to, entityIDs)
}

func (s *Service) BalanceSheet(ctx context.Context, asOf *time.Time, entityIDs []uuid.UUID) ([]repository.BalanceSheetSection, error) {
	return s.repo.BalanceSheet(ctx, asOf, entityIDs)
}

// EntityScope resolves which entity ids a report should cover: the current
// entity, or it plus its descendants when consolidated.
func (s *Service) EntityScope(ctx context.Context, consolidated bool) ([]uuid.UUID, error) {
	return s.repo.EntityScope(ctx, repository.EntityFromContext(ctx), consolidated)
}

// UpsertBudget sets an account's budget for a period (entity from context).
func (s *Service) UpsertBudget(ctx context.Context, period, accountCode, amount string) error {
	return s.repo.UpsertBudget(ctx, period, accountCode, amount)
}

// BudgetVsActual compares budget to actual per account over a window.
func (s *Service) BudgetVsActual(ctx context.Context, from, to *time.Time, entityIDs []uuid.UUID) ([]repository.BudgetLine, error) {
	return s.repo.BudgetVsActual(ctx, from, to, entityIDs)
}

// CashFlow summarises cash movement by activity category over a window.
func (s *Service) CashFlow(ctx context.Context, from, to *time.Time, entityIDs []uuid.UUID) ([]repository.CashFlowRow, error) {
	return s.repo.CashFlow(ctx, from, to, entityIDs)
}

// IndirectCashFlow reconciles net income to operating cash (IAS 7 indirect method).
func (s *Service) IndirectCashFlow(ctx context.Context, from, to *time.Time, entityIDs []uuid.UUID) (repository.IndirectCashFlowReport, error) {
	return s.repo.IndirectCashFlow(ctx, from, to, entityIDs)
}

func (s *Service) CreateProject(ctx context.Context, code, name string) (*repository.Dimension, error) {
	return s.repo.CreateProject(ctx, code, name)
}
func (s *Service) ListProjects(ctx context.Context) ([]repository.Dimension, error) {
	return s.repo.ListProjects(ctx)
}
func (s *Service) CreateCostCenter(ctx context.Context, code, name string) (*repository.Dimension, error) {
	return s.repo.CreateCostCenter(ctx, code, name)
}
func (s *Service) ListCostCenters(ctx context.Context) ([]repository.Dimension, error) {
	return s.repo.ListCostCenters(ctx)
}

// ProjectPL is the revenue/expense detail for a project over a window.
func (s *Service) ProjectPL(ctx context.Context, projectID uuid.UUID, from, to *time.Time) ([]repository.PLRow, error) {
	return s.repo.ProjectPL(ctx, projectID, from, to)
}

func (s *Service) CreateCustomer(ctx context.Context, code, name, email, phone, currency string) (*repository.Party, error) {
	return s.repo.CreateCustomer(ctx, code, name, email, phone, currency)
}
func (s *Service) ListCustomers(ctx context.Context) ([]repository.Party, error) {
	return s.repo.ListCustomers(ctx)
}
func (s *Service) CreateVendor(ctx context.Context, code, name, email, phone, currency string) (*repository.Party, error) {
	return s.repo.CreateVendor(ctx, code, name, email, phone, currency)
}
func (s *Service) ListVendors(ctx context.Context) ([]repository.Party, error) {
	return s.repo.ListVendors(ctx)
}

func (s *Service) CustomerEmailByRef(ctx context.Context, ref string) (string, error) {
	return s.repo.CustomerEmailByRef(ctx, ref)
}

func (s *Service) SalesByItem(ctx context.Context, from, to *time.Time, entityIDs []uuid.UUID) ([]repository.SalesByItemRow, error) {
	return s.repo.SalesByItem(ctx, from, to, entityIDs)
}
func (s *Service) StatementOfChangesInEquity(ctx context.Context, from, to *time.Time, entityIDs []uuid.UUID) ([]repository.EquityChangeRow, error) {
	return s.repo.StatementOfChangesInEquity(ctx, from, to, entityIDs)
}

// Entities lists configured accounting entities.
func (s *Service) Entities(ctx context.Context) ([]repository.Entity, error) {
	return s.repo.ListEntities(ctx)
}

// CreateEntity registers a new accounting entity.
func (s *Service) CreateEntity(ctx context.Context, code, name, baseCurrency string, parentID *uuid.UUID, ownershipPct string) (*repository.Entity, error) {
	return s.repo.CreateEntity(ctx, code, name, baseCurrency, parentID, ownershipPct)
}

// SetEntityOwnership updates the parent's ownership fraction of an entity (for
// consolidation elimination + NCI sizing).
func (s *Service) SetEntityOwnership(ctx context.Context, id uuid.UUID, ownershipPct string) (*repository.Entity, error) {
	return s.repo.SetEntityOwnership(ctx, id, ownershipPct)
}

func (s *Service) FinanceSummary(ctx context.Context) (repository.FinanceSummary, error) {
	return s.repo.FinanceSummary(ctx)
}

func (s *Service) CreateJournalEntry(ctx context.Context, in CreateEntryInput) (*domain.JournalEntry, error) {
	if len(in.Lines) == 0 {
		return nil, ErrEmptyEntry
	}
	if err := validateBalance(in.Lines); err != nil {
		return nil, err
	}

	if in.SourceEventID != nil {
		processed, err := s.repo.IsEventProcessed(ctx, *in.SourceEventID)
		if err != nil {
			return nil, err
		}
		if processed {
			return nil, ErrDuplicateEvent
		}
	}

	resolved, err := s.resolveLines(ctx, in.Lines)
	if err != nil {
		return nil, err
	}

	entryNumber, err := s.repo.NextEntryNumber(ctx)
	if err != nil {
		return nil, err
	}

	var accDate time.Time
	if in.AccountingDate != nil {
		accDate = in.AccountingDate.UTC()
	}
	return s.repo.CreateJournalEntry(ctx, repository.CreateJournalParams{
		EntryNumber:          entryNumber,
		Description:          in.Description,
		Status:               "draft",
		SourceEventID:        in.SourceEventID,
		SourceService:        in.SourceService,
		CorrelationID:        in.CorrelationID,
		CreatedBy:            in.CreatedBy,
		AccountingDate:       accDate,
		Lines:                resolved,
		CounterpartyEntityID: in.CounterpartyEntityID,
	})
}

func (s *Service) PostJournalEntry(ctx context.Context, id uuid.UUID, actor string) (*domain.JournalEntry, error) {
	entry, err := s.repo.GetJournalEntry(ctx, id)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return nil, fmt.Errorf("journal entry not found")
	}
	if entry.Status != "draft" {
		return nil, ErrInvalidStatus
	}
	if err := validateEntryLines(entry.Lines); err != nil {
		return nil, err
	}
	// Guarded, single-statement transition inside one tx: the WHERE
	// status='draft' filter is the race guard (a concurrent post matches zero
	// rows), and the fiscal-period close is checked against the entry's own
	// accounting_date — not wall-clock — so closing a month protects entries
	// dated to it regardless of when they are posted.
	postingDate := time.Now().UTC()
	posted, err := s.repo.MarkEntryPosted(ctx, id, postingDate, &repository.AuditInfo{
		Actor:     actorOrSystem(actor),
		EventType: "ledger.posted",
		Message:   fmt.Sprintf("posted %s (%s)", entry.EntryNumber, entry.Description),
	})
	if err != nil {
		if errors.Is(err, repository.ErrPeriodClosed) {
			return nil, ErrPeriodClosed
		}
		return nil, err
	}
	if !posted {
		return nil, ErrInvalidStatus
	}
	return s.repo.GetJournalEntry(ctx, id)
}

// actorOrSystem falls back to a non-empty system actor so a chain entry is
// always attributed even on unauthenticated/internal paths.
func actorOrSystem(actor string) string {
	if actor == "" {
		return "system"
	}
	return actor
}

// ReverseJournalEntry posts a reversing entry that cancels a posted entry and
// marks the original 'reversed'. The correct, audit-friendly way to undo a
// posted entry — the GL is never edited in place.
func (s *Service) ReverseJournalEntry(ctx context.Context, id uuid.UUID, reason, actor string) (*domain.JournalEntry, error) {
	return s.repo.ReverseEntry(ctx, id, reason, &repository.AuditInfo{
		Actor:     actorOrSystem(actor),
		EventType: "ledger.reversed",
		Message:   fmt.Sprintf("reversed entry %s: %s", id, reason),
	})
}

// FiscalPeriods lists every period with an explicit open/closed status.
func (s *Service) FiscalPeriods(ctx context.Context) ([]repository.FiscalPeriod, error) {
	return s.repo.ListPeriods(ctx)
}

// ClosePeriod blocks further postings dated in 'YYYY-MM'.
func (s *Service) ClosePeriod(ctx context.Context, period string, by *uuid.UUID) error {
	return s.repo.SetPeriodStatus(ctx, period, "closed", by)
}

// ReopenPeriod lifts a close so postings into 'YYYY-MM' are allowed again.
func (s *Service) ReopenPeriod(ctx context.Context, period string, by *uuid.UUID) error {
	return s.repo.SetPeriodStatus(ctx, period, "open", by)
}

func (s *Service) BookFromEvent(ctx context.Context, eventID, eventType, source, correlationID, description, currency string, lines []LineInput) (*domain.JournalEntry, error) {
	if len(lines) == 0 {
		return nil, ErrEmptyEntry
	}
	if err := validateBalance(lines); err != nil {
		return nil, err
	}

	resolved, err := s.resolveLines(ctx, lines)
	if err != nil {
		return nil, err
	}

	// Posting date is "now" — the instant the event hits the ledger. Refuse to
	// book into a period an operator has closed (open by default).
	postingDate := time.Now().UTC()
	closed, err := s.repo.IsPeriodClosed(ctx, postingDate.Format("2006-01"))
	if err != nil {
		return nil, err
	}
	if closed {
		return nil, ErrPeriodClosed
	}

	// Convert to base at the event-date rate (defaults to 1 for base currency).
	fxRate := s.repo.RateOrOne(ctx, currency, postingDate)

	// Single transaction: entry + lines + processed_events commit together, and
	// the partial-unique index on source_event_id makes concurrent redelivery
	// idempotent (returns the already-booked entry instead of duplicating).
	return s.repo.BookPostedEntry(ctx, repository.CreateJournalParams{
		Description:   description,
		SourceEventID: &eventID,
		SourceService: optionalString(source),
		CorrelationID: optionalString(correlationID),
		Currency:      currency,
		FXRate:        fxRate,
		Lines:         resolved,
	}, eventID, eventType, postingDate, nil, &repository.AuditInfo{
		Actor:     "system:" + source,
		EventType: "ledger.booked",
		Message:   fmt.Sprintf("%s booked from %s (%s)", eventType, source, description),
	})
}

func validateBalance(lines []LineInput) error {
	var debit, credit decimal.Decimal
	for _, l := range lines {
		debit = debit.Add(l.Debit)
		credit = credit.Add(l.Credit)
	}
	if debit.IsZero() && credit.IsZero() {
		return ErrEmptyEntry
	}
	if !debit.Equal(credit) {
		return ErrUnbalancedEntry
	}
	return nil
}

func validateEntryLines(lines []domain.JournalLine) error {
	var debit, credit decimal.Decimal
	for _, l := range lines {
		debit = debit.Add(l.Debit)
		credit = credit.Add(l.Credit)
	}
	if debit.IsZero() && credit.IsZero() {
		return ErrEmptyEntry
	}
	if !debit.Equal(credit) {
		return ErrUnbalancedEntry
	}
	return nil
}

func (s *Service) resolveLines(ctx context.Context, lines []LineInput) ([]repository.ResolvedLine, error) {
	out := make([]repository.ResolvedLine, 0, len(lines))
	for i, l := range lines {
		acct, err := s.repo.GetAccountByCode(ctx, l.AccountCode)
		if err != nil {
			return nil, err
		}
		if acct == nil {
			return nil, fmt.Errorf("%w: %s", ErrAccountNotFound, l.AccountCode)
		}
		if acct.RestrictToNaturalSide && violatesNaturalSide(acct.AccountType, l.Debit, l.Credit) {
			return nil, fmt.Errorf("%w: %s (%s) may only be posted on its natural side", ErrUnnaturalSide, l.AccountCode, acct.AccountType)
		}
		out = append(out, repository.ResolvedLine{
			AccountID:    acct.ID,
			Debit:        l.Debit,
			Credit:       l.Credit,
			Memo:         l.Memo,
			LineOrder:    i,
			CostCenterID: l.CostCenterID,
			ProjectID:    l.ProjectID,
		})
	}
	return out, nil
}

func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// violatesNaturalSide reports whether a line posts to an account's non-natural
// side: asset/expense are debit-normal, liability/equity/revenue credit-normal.
// Only consulted for accounts explicitly opted into restrict_to_natural_side.
func violatesNaturalSide(accountType string, debit, credit decimal.Decimal) bool {
	switch accountType {
	case "asset", "expense":
		return credit.IsPositive()
	case "liability", "equity", "revenue":
		return debit.IsPositive()
	default:
		return false
	}
}
