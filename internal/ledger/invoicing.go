package ledger

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/repository"
)

// ErrInvoiceNotDraft blocks issuing an invoice that is not in draft.
var ErrInvoiceNotDraft = errors.New("only a draft invoice can be issued")

func (s *Service) CreateInvoice(ctx context.Context, in repository.CreateInvoiceInput) (*repository.Invoice, error) {
	return s.repo.CreateInvoice(ctx, in)
}

func (s *Service) GetInvoice(ctx context.Context, id uuid.UUID) (*repository.Invoice, error) {
	return s.repo.GetInvoice(ctx, id)
}

func (s *Service) ListInvoices(ctx context.Context, limit, offset int) ([]repository.Invoice, error) {
	return s.repo.ListInvoices(ctx, limit, offset)
}

func (s *Service) CreateRecurringInvoice(ctx context.Context, in repository.CreateRecurringInput) (*repository.RecurringInvoice, error) {
	return s.repo.CreateRecurringInvoice(ctx, in)
}

func (s *Service) ListRecurringInvoices(ctx context.Context) ([]repository.RecurringInvoice, error) {
	return s.repo.ListRecurringInvoices(ctx)
}

// IssueInvoice posts a draft invoice: it books the GL (Dr AR / Cr Revenue / Cr
// Output VAT), creates the linked AR open item, and marks the invoice issued.
// Idempotent — a non-draft invoice is rejected. Entity/FX come from the booking.
func (s *Service) IssueInvoice(ctx context.Context, id uuid.UUID, actor string) (*repository.Invoice, error) {
	inv, err := s.repo.GetInvoice(ctx, id)
	if err != nil {
		return nil, err
	}
	if inv == nil {
		return nil, repository.ErrInvoiceNotFound
	}
	if inv.Status != "draft" {
		return nil, ErrInvoiceNotDraft
	}

	subtotal, _ := decimal.NewFromString(inv.Subtotal)
	taxTotal, _ := decimal.NewFromString(inv.TaxTotal)
	total, _ := decimal.NewFromString(inv.Total)

	lineInputs := []LineInput{
		{AccountCode: "1100", Debit: total, Memo: "AR — " + inv.Number},
		{AccountCode: "4000", Credit: subtotal, Memo: "Revenue"},
	}
	if taxTotal.IsPositive() {
		lineInputs = append(lineInputs, LineInput{AccountCode: "2100", Credit: taxTotal, Memo: "Output VAT"})
	}
	resolved, err := s.resolveLines(ctx, lineInputs)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	rate := s.repo.RateOrOne(ctx, inv.Currency, now)
	eventID := "invoice.issued:" + id.String()
	src := "iag.finance"
	entry, err := s.repo.BookPostedEntry(ctx, repository.CreateJournalParams{
		Description: "Invoice " + inv.Number, SourceService: &src, SourceEventID: &eventID,
		Currency: inv.Currency, FXRate: rate, Lines: resolved,
	}, eventID, "finance.invoice.issued", now, nil, &repository.AuditInfo{
		Actor: actorOrSystem(actor), EventType: "invoice.issued", Message: "issued " + inv.Number,
	})
	if err != nil {
		return nil, err
	}

	// Linked AR open item (idempotent on document_ref = invoice number).
	if _, err := s.repo.CreateAROpenItem(ctx, inv.CustomerRef, inv.Number, "Invoice "+inv.Number,
		total.String(), inv.Currency, inv.DueDate, &entry.ID, &eventID, nil); err != nil && !repository.IsUniqueViolation(err) {
		return nil, err
	}

	if err := s.repo.MarkInvoiceIssued(ctx, id, inv.Number, now); err != nil {
		return nil, err
	}

	// IFRS 15: when the invoice carries a ratable recognition spec, defer the
	// subtotal and spread it over the schedule (Dr 4000 / Cr 2300 now, then a
	// monthly Dr 2300 / Cr 4000 recognition run). Idempotent on the invoice
	// number, so a re-issue never double-defers.
	if inv.RecognitionMethod == "ratable" && inv.RecognitionPeriods >= 1 && subtotal.IsPositive() {
		start := inv.RecognitionStart
		if start == "" {
			start = now.Format("2006-01")
		}
		if _, err := s.repo.CreateSchedule(ctx, repository.CreateScheduleInput{
			SourceRef:   inv.Number,
			Total:       subtotal,
			Currency:    inv.Currency,
			Method:      "ratable",
			StartPeriod: start,
			Periods:     inv.RecognitionPeriods,
		}, now, &repository.AuditInfo{
			Actor: actorOrSystem(actor), EventType: "finance.revenue.defer", Message: "defer invoice " + inv.Number,
		}); err != nil && !errors.Is(err, repository.ErrScheduleExists) {
			return nil, err
		}
	}
	return s.repo.GetInvoice(ctx, id)
}
