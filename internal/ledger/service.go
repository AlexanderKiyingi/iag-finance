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
)

type LineInput struct {
	AccountCode string
	Debit       decimal.Decimal
	Credit      decimal.Decimal
	Memo        string
}

type CreateEntryInput struct {
	Description   string
	Lines         []LineInput
	SourceEventID *string
	SourceService *string
	CorrelationID *string
	CreatedBy     *uuid.UUID
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

func (s *Service) ListJournalEntries(ctx context.Context, limit, offset int) ([]domain.JournalEntry, error) {
	return s.repo.ListJournalEntries(ctx, limit, offset)
}

func (s *Service) GetJournalEntry(ctx context.Context, id uuid.UUID) (*domain.JournalEntry, error) {
	return s.repo.GetJournalEntry(ctx, id)
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

func (s *Service) CreateARItem(ctx context.Context, customerRef, documentRef, description, amount, currency string, dueDate *time.Time) (*domain.AROpenItem, error) {
	return s.repo.CreateAROpenItem(ctx, customerRef, documentRef, description, amount, currency, dueDate, nil, nil)
}

func (s *Service) CreateARItemWithBilling(ctx context.Context, customerRef, documentRef, description, amount, currency string, dueDate *time.Time, billingOrgID, billingIdentityID *uuid.UUID) (*domain.AROpenItem, error) {
	return s.repo.CreateAROpenItemWithBilling(ctx, customerRef, documentRef, description, amount, currency, dueDate, billingOrgID, billingIdentityID)
}

func (s *Service) GetAROpenItemByID(ctx context.Context, id uuid.UUID) (*domain.AROpenItem, error) {
	return s.repo.GetAROpenItem(ctx, id)
}

func (s *Service) CreateAPItem(ctx context.Context, vendorRef, documentRef, description, amount, currency string, dueDate *time.Time) (*domain.APOpenItem, error) {
	return s.repo.CreateAPOpenItem(ctx, vendorRef, documentRef, description, amount, currency, dueDate, nil, nil)
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

func (s *Service) TrialBalance(ctx context.Context) ([]repository.TrialBalanceRow, error) {
	return s.repo.TrialBalance(ctx)
}

func (s *Service) ARAging(ctx context.Context) ([]repository.ARAgingBucket, error) {
	return s.repo.ARAging(ctx)
}

func (s *Service) ProfitAndLoss(ctx context.Context) ([]repository.PLRow, error) {
	return s.repo.ProfitAndLoss(ctx)
}

func (s *Service) BalanceSheet(ctx context.Context) ([]repository.BalanceSheetSection, error) {
	return s.repo.BalanceSheet(ctx)
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

	return s.repo.CreateJournalEntry(ctx, repository.CreateJournalParams{
		EntryNumber:   entryNumber,
		Description:   in.Description,
		Status:        "draft",
		SourceEventID: in.SourceEventID,
		SourceService: in.SourceService,
		CorrelationID: in.CorrelationID,
		CreatedBy:     in.CreatedBy,
		Lines:         resolved,
	})
}

func (s *Service) PostJournalEntry(ctx context.Context, id uuid.UUID) (*domain.JournalEntry, error) {
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
	// Period-close control: refuse to post into a month an operator has
	// closed. Periods are open by default, so this only bites once a close
	// has been issued. The posting date is "now" — when the entry hits the
	// ledger — which is the date that determines the period.
	postingDate := time.Now().UTC()
	closed, err := s.repo.IsPeriodClosed(ctx, postingDate.Format("2006-01"))
	if err != nil {
		return nil, err
	}
	if closed {
		return nil, ErrPeriodClosed
	}
	return s.repo.UpdateJournalStatus(ctx, id, "posted", postingDate)
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

func (s *Service) BookFromEvent(ctx context.Context, eventID, eventType, source, correlationID, description string, lines []LineInput) (*domain.JournalEntry, error) {
	processed, err := s.repo.IsEventProcessed(ctx, eventID)
	if err != nil {
		return nil, err
	}
	if processed {
		return s.repo.GetJournalEntryBySourceEvent(ctx, eventID)
	}

	entry, err := s.CreateJournalEntry(ctx, CreateEntryInput{
		Description:   description,
		Lines:         lines,
		SourceEventID: &eventID,
		SourceService: &source,
		CorrelationID: optionalString(correlationID),
	})
	if err != nil {
		if errors.Is(err, ErrDuplicateEvent) {
			return s.repo.GetJournalEntryBySourceEvent(ctx, eventID)
		}
		return nil, err
	}

	posted, err := s.PostJournalEntry(ctx, entry.ID)
	if err != nil {
		return nil, err
	}
	if err := s.repo.MarkEventProcessed(ctx, eventID, eventType); err != nil {
		return nil, err
	}
	return posted, nil
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
		out = append(out, repository.ResolvedLine{
			AccountID: acct.ID,
			Debit:     l.Debit,
			Credit:    l.Credit,
			Memo:      l.Memo,
			LineOrder: i,
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
