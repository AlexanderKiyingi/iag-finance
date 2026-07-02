package ledger

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/domain"
	"github.com/iag-finance/backend/internal/repository"
)

const (
	grniExpenseAccount  = "5000" // expense / COGS (goods received)
	grIRClearingAccount = "2150" // GR/IR clearing (liability)
	inputVATAccount     = "1300" // recoverable input VAT (purchases)
	outputVATAccount    = "2100" // output VAT (payable) — used for reverse charge
	apControlAccount    = "2000" // accounts payable
)

// BookGRNAccrual accrues the AP liability at goods receipt: Dr expense / Cr GR-IR
// clearing for the received value, and raises the per-PO open accrual so a later
// invoice can clear it. No-op without a PO reference (the accrual could never be
// cleared) or a positive value. Idempotent on eventID via the shared booking
// primitive; the accrual bump runs as a side-effect in the same transaction.
func (s *Service) BookGRNAccrual(ctx context.Context, eventID, eventType, source, correlationID, currency, poRef string, value decimal.Decimal) (*domain.JournalEntry, error) {
	if poRef == "" || value.LessThanOrEqual(decimal.Zero) {
		return nil, nil
	}
	if currency == "" {
		currency = s.repo.BaseCurrency()
	}
	resolved, err := s.resolveLines(ctx, []LineInput{
		{AccountCode: grniExpenseAccount, Debit: value, Memo: "Goods received (GRNI)"},
		{AccountCode: grIRClearingAccount, Credit: value, Memo: "GR/IR accrual"},
	})
	if err != nil {
		return nil, err
	}
	postingDate := time.Now().UTC()
	if closed, err := s.repo.IsPeriodClosed(ctx, postingDate.Format("2006-01")); err != nil {
		return nil, err
	} else if closed {
		return nil, ErrPeriodClosed
	}
	side := func(ctx context.Context, tx pgx.Tx, _ uuid.UUID) error {
		return repository.AddGRNIAccrualTx(ctx, tx, poRef, currency, value)
	}
	return s.repo.BookPostedEntry(ctx, repository.CreateJournalParams{
		Description:   "Goods received accrual — PO " + poRef,
		SourceEventID: &eventID,
		SourceService: optionalString(source),
		CorrelationID: optionalString(correlationID),
		Currency:      currency,
		Lines:         resolved,
	}, eventID, eventType, postingDate, side, &repository.AuditInfo{
		Actor:     "system:" + source,
		EventType: "ledger.grni.accrued",
		Message:   "GR/IR accrual for PO " + poRef,
	})
}

// BookAPInvoice books a vendor invoice as AP. The credit is always the gross
// payable; vat is the VAT portion already included in gross (net = gross − vat,
// zero when the event carries no VAT) and is debited to the VAT control account.
// The net debit is routed by whether the invoice references a PO:
//   - With a PO ref, the full net debits the GR/IR clearing account — expense for
//     the goods is recognised solely by the matching goods-receipt accrual (Dr
//     expense / Cr GR-IR). The clearing nets to zero once both sides post, in
//     EITHER order, so the expense is booked exactly once and an invoice that
//     beats its GRN never double-counts.
//   - Without a PO ref (services, fuel, ad-hoc), the net debits expense directly.
//
// poRef "" + vat 0 reduces to the prior Dr expense / Cr AP. Idempotent on eventID;
// the accrual-clearing bookkeeping runs as a side-effect in the same transaction.
//
// reverseCharge marks a supply where the buyer self-assesses VAT (the supplier
// charges none): the AP liability is the net only, and the buyer books both
// recoverable input VAT and payable output VAT for net × the taxCode's rate — a
// net-zero cash effect added to the same entry, so the reverse-charge VAT is
// recognised exactly once alongside the AP booking.
func (s *Service) BookAPInvoice(ctx context.Context, eventID, eventType, source, correlationID, description, currency, poRef string, gross, vat decimal.Decimal, reverseCharge bool, taxCode string) (*domain.JournalEntry, error) {
	if gross.LessThanOrEqual(decimal.Zero) {
		return nil, ErrEmptyEntry
	}
	if vat.IsNegative() || vat.GreaterThan(gross) {
		vat = decimal.Zero // ignore a nonsensical VAT amount rather than misbook
	}
	net := gross.Sub(vat)

	// Reverse charge: the supplier bills no VAT, so gross is the net payable and
	// the buyer self-assesses VAT on it from the tax code's rate.
	rcVAT := decimal.Zero
	if reverseCharge && taxCode != "" {
		rate, _, ok, err := s.repo.GetTaxCode(ctx, taxCode)
		if err != nil {
			return nil, err
		}
		if ok {
			rcVAT = net.Mul(rate).Round(2)
		}
	}

	lines := make([]LineInput, 0, 5)
	netToGRIR := poRef != "" && net.IsPositive()
	if netToGRIR {
		lines = append(lines, LineInput{AccountCode: grIRClearingAccount, Debit: net, Memo: "GR/IR clearing"})
	} else if net.IsPositive() {
		lines = append(lines, LineInput{AccountCode: grniExpenseAccount, Debit: net, Memo: "Expense / COGS"})
	}
	if vat.IsPositive() {
		lines = append(lines, LineInput{AccountCode: inputVATAccount, Debit: vat, Memo: "Input VAT"})
	}
	lines = append(lines, LineInput{AccountCode: apControlAccount, Credit: gross, Memo: "AP liability"})
	if rcVAT.IsPositive() {
		// Self-assessed reverse-charge VAT: recoverable input vs payable output.
		lines = append(lines,
			LineInput{AccountCode: inputVATAccount, Debit: rcVAT, Memo: "Reverse-charge input VAT"},
			LineInput{AccountCode: outputVATAccount, Credit: rcVAT, Memo: "Reverse-charge output VAT"},
		)
	}

	if err := validateBalance(lines); err != nil {
		return nil, err
	}
	resolved, err := s.resolveLines(ctx, lines)
	if err != nil {
		return nil, err
	}
	postingDate := time.Now().UTC()
	if closed, err := s.repo.IsPeriodClosed(ctx, postingDate.Format("2006-01")); err != nil {
		return nil, err
	} else if closed {
		return nil, ErrPeriodClosed
	}
	if currency == "" {
		currency = s.repo.BaseCurrency()
	}

	var side repository.BookSideEffect
	if netToGRIR {
		cleared := net
		side = func(ctx context.Context, tx pgx.Tx, _ uuid.UUID) error {
			return repository.ClearGRNIAccrualTx(ctx, tx, poRef, currency, cleared)
		}
	}
	return s.repo.BookPostedEntry(ctx, repository.CreateJournalParams{
		Description:   description,
		SourceEventID: &eventID,
		SourceService: optionalString(source),
		CorrelationID: optionalString(correlationID),
		Currency:      currency,
		Lines:         resolved,
	}, eventID, eventType, postingDate, side, &repository.AuditInfo{
		Actor:     "system:" + source,
		EventType: "ledger.booked",
		Message:   description,
	})
}
