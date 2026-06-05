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

func (s *Service) CreateAPItem(ctx context.Context, vendorRef, documentRef, description, amount, currency string, dueDate *time.Time) (*domain.APOpenItem, error) {
	return s.repo.CreateAPOpenItem(ctx, vendorRef, documentRef, description, amount, currency, dueDate, nil, nil)
}

func (s *Service) TrialBalance(ctx context.Context) ([]repository.TrialBalanceRow, error) {
	return s.repo.TrialBalance(ctx)
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
	return s.repo.UpdateJournalStatus(ctx, id, "posted", time.Now().UTC())
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
