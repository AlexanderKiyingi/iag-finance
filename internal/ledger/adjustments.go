package ledger

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/domain"
	"github.com/iag-finance/backend/internal/repository"
)

var (
	ErrInvalidAdjustment = errors.New("invalid adjustment")
)

type AdjustmentInput struct {
	Kind                string
	Direction           string
	OriginalDocumentRef string
	DocumentRef         string
	Reason              string
	Amount              decimal.Decimal
	Currency            string
}

func (s *Service) CreateAdjustment(ctx context.Context, in AdjustmentInput) (*domain.Adjustment, error) {
	in.Kind = strings.ToLower(strings.TrimSpace(in.Kind))
	in.Direction = strings.ToLower(strings.TrimSpace(in.Direction))
	if in.Kind != "credit_note" && in.Kind != "debit_note" {
		return nil, ErrInvalidAdjustment
	}
	if in.Direction != "ar" && in.Direction != "ap" {
		return nil, ErrInvalidAdjustment
	}
	if in.OriginalDocumentRef == "" || in.Amount.LessThanOrEqual(decimal.Zero) {
		return nil, ErrInvalidAdjustment
	}
	docRef := strings.TrimSpace(in.DocumentRef)
	if docRef == "" {
		prefix := "CN"
		if in.Kind == "debit_note" {
			prefix = "DN"
		}
		docRef = fmt.Sprintf("%s-%s", prefix, in.OriginalDocumentRef)
	}

	var partyRef string
	var delta decimal.Decimal
	var lines []LineInput
	desc := fmt.Sprintf("%s — %s", in.Kind, in.OriginalDocumentRef)

	switch in.Direction + ":" + in.Kind {
	case "ar:credit_note":
		ar, err := s.repo.GetARByDocumentRef(ctx, in.OriginalDocumentRef)
		if err != nil || ar == nil {
			return nil, repository.ErrOriginalNotFound
		}
		partyRef = ar.CustomerRef
		delta = in.Amount.Neg()
		lines = []LineInput{
			{AccountCode: "4000", Debit: in.Amount, Memo: "Credit note"},
			{AccountCode: "1100", Credit: in.Amount, Memo: "AR credit"},
		}
	case "ar:debit_note":
		ar, err := s.repo.GetARByDocumentRef(ctx, in.OriginalDocumentRef)
		if err != nil || ar == nil {
			return nil, repository.ErrOriginalNotFound
		}
		partyRef = ar.CustomerRef
		delta = in.Amount
		lines = []LineInput{
			{AccountCode: "1100", Debit: in.Amount, Memo: "AR debit"},
			{AccountCode: "4000", Credit: in.Amount, Memo: "Debit note"},
		}
	case "ap:credit_note":
		ap, err := s.repo.GetAPByDocumentRef(ctx, in.OriginalDocumentRef)
		if err != nil || ap == nil {
			return nil, repository.ErrOriginalNotFound
		}
		partyRef = ap.VendorRef
		delta = in.Amount.Neg()
		lines = []LineInput{
			{AccountCode: "2000", Debit: in.Amount, Memo: "AP credit"},
			{AccountCode: "5100", Credit: in.Amount, Memo: "Vendor credit note"},
		}
	case "ap:debit_note":
		ap, err := s.repo.GetAPByDocumentRef(ctx, in.OriginalDocumentRef)
		if err != nil || ap == nil {
			return nil, repository.ErrOriginalNotFound
		}
		partyRef = ap.VendorRef
		delta = in.Amount
		lines = []LineInput{
			{AccountCode: "5100", Debit: in.Amount, Memo: "Vendor debit note"},
			{AccountCode: "2000", Credit: in.Amount, Memo: "AP debit"},
		}
	default:
		return nil, ErrInvalidAdjustment
	}

	currency := in.Currency
	if currency == "" {
		currency = "UGX"
	}

	eventID := fmt.Sprintf("adjustment.%s:%s:%s", in.Direction, in.Kind, docRef)
	resolved, err := s.resolveLines(ctx, lines)
	if err != nil {
		return nil, err
	}

	entryNumber, err := s.repo.NextEntryNumber(ctx)
	if err != nil {
		return nil, err
	}
	entry, err := s.repo.CreateJournalEntry(ctx, repository.CreateJournalParams{
		EntryNumber: entryNumber,
		Description: desc,
		Status:      "draft",
		SourceEventID: &eventID,
		SourceService: strPtr("iag.finance"),
		Lines:         resolved,
	})
	if err != nil {
		return nil, err
	}
	posted, err := s.PostJournalEntry(ctx, entry.ID)
	if err != nil {
		return nil, err
	}

	if in.Direction == "ar" {
		if err := s.repo.AdjustARAmount(ctx, in.OriginalDocumentRef, delta); err != nil {
			return nil, err
		}
	} else {
		if err := s.repo.AdjustAPAmount(ctx, in.OriginalDocumentRef, delta); err != nil {
			return nil, err
		}
	}

	return s.repo.CreateAdjustment(ctx, repository.CreateAdjustmentParams{
		Kind:                in.Kind,
		Direction:           in.Direction,
		OriginalDocumentRef: in.OriginalDocumentRef,
		DocumentRef:         docRef,
		PartyRef:            partyRef,
		Amount:              in.Amount,
		Currency:            currency,
		Reason:              in.Reason,
		JournalEntryID:      posted.ID,
	})
}

func (s *Service) ListAdjustments(ctx context.Context, originalRef, direction string, limit int) ([]domain.Adjustment, error) {
	return s.repo.ListAdjustments(ctx, originalRef, direction, limit)
}

func (s *Service) ListARByCustomerRef(ctx context.Context, customerRef string, limit, offset int) ([]domain.AROpenItem, error) {
	return s.repo.ListARByCustomerRef(ctx, customerRef, limit, offset)
}

func (s *Service) EnsurePaymentLinkToken(ctx context.Context, itemID uuid.UUID) (string, error) {
	return s.repo.EnsurePaymentLinkToken(ctx, itemID)
}

func strPtr(s string) *string { return &s }
