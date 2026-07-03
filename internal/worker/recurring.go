package worker

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/ledger"
	"github.com/iag-finance/backend/internal/repository"
)

// RecurringInvoicer generates and issues invoices from due recurring schedules.
type RecurringInvoicer struct {
	repo     *repository.Repository
	ledger   *ledger.Service
	interval time.Duration
}

func NewRecurringInvoicer(repo *repository.Repository, ledgerSvc *ledger.Service, interval time.Duration) *RecurringInvoicer {
	if interval <= 0 {
		interval = time.Hour
	}
	return &RecurringInvoicer{repo: repo, ledger: ledgerSvc, interval: interval}
}

func (w *RecurringInvoicer) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		w.generate(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

type recurringTemplateLine struct {
	Description string `json:"description"`
	Quantity    string `json:"quantity"`
	UnitPrice   string `json:"unitPrice"`
	TaxCode     string `json:"taxCode"`
}

func (w *RecurringInvoicer) generate(ctx context.Context) {
	due, err := w.repo.ListDueRecurring(ctx, time.Now().UTC())
	if err != nil {
		slog.Error("recurring: list due failed", "err", err)
		return
	}
	for _, s := range due {
		// Generate the invoice within the schedule's entity.
		ectx := repository.WithEntity(ctx, s.EntityID)

		var tmpl []recurringTemplateLine
		if err := json.Unmarshal(s.Template, &tmpl); err != nil {
			slog.Error("recurring: bad template", "schedule", s.ID, "err", err)
			continue
		}
		lines := make([]repository.InvoiceLineInput, 0, len(tmpl))
		for _, l := range tmpl {
			qty := decimal.NewFromInt(1)
			if d, err := decimal.NewFromString(l.Quantity); err == nil {
				qty = d
			}
			price, _ := decimal.NewFromString(l.UnitPrice)
			lines = append(lines, repository.InvoiceLineInput{
				Description: l.Description, Quantity: qty, UnitPrice: price, TaxCode: l.TaxCode,
			})
		}

		inv, err := w.ledger.CreateInvoice(ectx, repository.CreateInvoiceInput{
			CustomerRef: s.CustomerRef, Currency: s.Currency, Notes: s.Notes, Lines: lines,
			// Each generated invoice inherits the schedule's recognition and spreads
			// from its own issue month (empty start → IssueInvoice defaults it).
			RecognitionMethod: s.RecognitionMethod, RecognitionPeriods: s.RecognitionPeriods,
		})
		if err != nil {
			slog.Error("recurring: create invoice failed", "schedule", s.ID, "err", err)
			continue
		}
		if _, err := w.ledger.IssueInvoice(ectx, inv.ID, "system:recurring"); err != nil {
			slog.Error("recurring: issue invoice failed", "schedule", s.ID, "invoice", inv.ID, "err", err)
			continue
		}
		if err := w.repo.AdvanceRecurring(ctx, s.ID, advanceRun(s.NextRun, s.Cadence)); err != nil {
			slog.Error("recurring: advance failed", "schedule", s.ID, "err", err)
			continue
		}
		slog.Info("recurring invoice generated", "schedule", s.ID, "invoice", inv.Number)
	}
}

func advanceRun(d time.Time, cadence string) time.Time {
	if cadence == "weekly" {
		return d.AddDate(0, 0, 7)
	}
	return d.AddDate(0, 1, 0)
}
