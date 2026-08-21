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

// grnAccrualLines splits the goods-receipt debit between stock and period
// expense, crediting GR/IR with the whole received value either way.
//
// Pure, so the split is testable without a ledger: this is the posting that used
// to double-count, and the arithmetic is the guarantee that it no longer can.
// inventoryValue is clamped into [0, value] — a nonsensical split from an
// upstream emitter must not be able to unbalance the entry or invent a credit.
func grnAccrualLines(value, inventoryValue decimal.Decimal) []LineInput {
	stock := inventoryValue
	if stock.IsNegative() {
		stock = decimal.Zero
	}
	if stock.GreaterThan(value) {
		stock = value
	}
	expense := value.Sub(stock)

	lines := make([]LineInput, 0, 3)
	if stock.IsPositive() {
		lines = append(lines, LineInput{AccountCode: inventoryAccount, Debit: stock, Memo: "Goods received into stock"})
	}
	if expense.IsPositive() {
		lines = append(lines, LineInput{AccountCode: grniExpenseAccount, Debit: expense, Memo: "Goods received (GRNI)"})
	}
	return append(lines, LineInput{AccountCode: grIRClearingAccount, Credit: value, Memo: "GR/IR accrual"})
}

const (
	grniExpenseAccount  = "5000" // expense / COGS (goods received)
	grIRClearingAccount = "2150" // GR/IR clearing (liability)
	inputVATAccount     = "1300" // recoverable input VAT (purchases)
	outputVATAccount    = "2100" // output VAT (payable) — used for reverse charge
	apControlAccount    = "2000" // accounts payable
)

// BookGRNAccrual accrues the AP liability at goods receipt — Cr GR/IR clearing
// for the received value — and raises the per-PO open accrual so a later invoice
// can clear it. No-op without a PO reference (the accrual could never be cleared)
// or a positive value. Idempotent on ref.ID via the shared booking primitive; the
// accrual bump runs as a side-effect in the same transaction.
//
// The debit is split: inventoryValue capitalises to stock, the remainder is
// period expense. This is the goods receipt's *only* posting — see
// grnReceiptLines for why the warehouse movement does not book one too.
// An event carrying no split debits everything to expense, which is what every
// emitter did before the split existed.
//
// The entry is dated by the receipt, not by when this consumer read the event,
// and converts to base at that date's rate. It previously carried no FX rate at
// all, so a foreign-currency receipt was recorded in base as though the figures
// were already base — a USD 1,000 accrual sat in the trial balance as UGX 1,000.
func (s *Service) BookGRNAccrual(ctx context.Context, ref EventRef, currency, poRef string, value, inventoryValue, qtyReceived decimal.Decimal) (*domain.JournalEntry, error) {
	if poRef == "" || value.LessThanOrEqual(decimal.Zero) {
		return nil, nil
	}
	if currency == "" {
		currency = s.repo.BaseCurrency()
	}
	resolved, err := s.resolveLines(ctx, grnAccrualLines(value, inventoryValue))
	if err != nil {
		return nil, err
	}
	window, err := s.resolvePostingWindow(ctx, ref)
	if err != nil {
		return nil, err
	}
	side := func(ctx context.Context, tx pgx.Tx, _ uuid.UUID) error {
		return repository.AddGRNIAccrualTx(ctx, tx, poRef, currency, value, qtyReceived)
	}
	return s.repo.BookPostedEntry(ctx, repository.CreateJournalParams{
		Description:    describeDeferral("Goods received accrual — PO "+poRef, window),
		SourceEventID:  &ref.ID,
		SourceService:  optionalString(ref.Source),
		CorrelationID:  optionalString(ref.CorrelationID),
		Currency:       currency,
		FXRate:         s.repo.RateOrOne(ctx, currency, window.Transaction),
		AccountingDate: window.Accounting,
		Lines:          resolved,
	}, ref.ID, ref.Type, time.Now().UTC(), side, &repository.AuditInfo{
		Actor:     "system:" + ref.Source,
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
//   - Without a PO ref (services, fuel, ad-hoc), the net debits expense — except
//     for the inventoryValue portion, which capitalises to stock. A coffee
//     cherry purchase is the case that needs this: it is bought as inventory and
//     there is no purchase order behind it, so expensing it would charge the
//     whole crop to profit on the day it arrived and leave the stock unrecorded.
//
// poRef "" + vat 0 reduces to the prior Dr expense / Cr AP. Idempotent on ref.ID;
// the accrual-clearing bookkeeping runs as a side-effect in the same transaction.
//
// Dated by the invoice event, and converted to base at that date's rate — this
// path also carried no FX rate before, so a foreign-currency payable was
// understated in base by whatever the rate was.
//
// reverseCharge marks a supply where the buyer self-assesses VAT (the supplier
// charges none): the AP liability is the net only, and the buyer books both
// recoverable input VAT and payable output VAT for net × the taxCode's rate — a
// net-zero cash effect added to the same entry, so the reverse-charge VAT is
// recognised exactly once alongside the AP booking.
func (s *Service) BookAPInvoice(ctx context.Context, ref EventRef, description, currency, poRef string, gross, vat, inventoryValue decimal.Decimal, reverseCharge bool, taxCode string, qtyInvoiced decimal.Decimal) (*domain.JournalEntry, error) {
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

	lines := make([]LineInput, 0, 6)
	netToGRIR := poRef != "" && net.IsPositive()
	switch {
	case netToGRIR:
		lines = append(lines, LineInput{AccountCode: grIRClearingAccount, Debit: net, Memo: "GR/IR clearing"})
	case net.IsPositive():
		// Split the net between stock and expense. Clamped into [0, net] so a
		// bad figure upstream cannot unbalance the entry or invent a debit.
		stock := inventoryValue
		if stock.IsNegative() {
			stock = decimal.Zero
		}
		if stock.GreaterThan(net) {
			stock = net
		}
		if stock.IsPositive() {
			lines = append(lines, LineInput{AccountCode: inventoryAccount, Debit: stock, Memo: "Purchased into stock"})
		}
		if expense := net.Sub(stock); expense.IsPositive() {
			lines = append(lines, LineInput{AccountCode: grniExpenseAccount, Debit: expense, Memo: "Expense / COGS"})
		}
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
	window, err := s.resolvePostingWindow(ctx, ref)
	if err != nil {
		return nil, err
	}
	if currency == "" {
		currency = s.repo.BaseCurrency()
	}

	var side repository.BookSideEffect
	if netToGRIR {
		cleared := net
		qty := qtyInvoiced
		side = func(ctx context.Context, tx pgx.Tx, _ uuid.UUID) error {
			return repository.ClearGRNIAccrualTx(ctx, tx, poRef, currency, cleared, qty)
		}
	}
	return s.repo.BookPostedEntry(ctx, repository.CreateJournalParams{
		Description:    describeDeferral(description, window),
		SourceEventID:  &ref.ID,
		SourceService:  optionalString(ref.Source),
		CorrelationID:  optionalString(ref.CorrelationID),
		Currency:       currency,
		FXRate:         s.repo.RateOrOne(ctx, currency, window.Transaction),
		AccountingDate: window.Accounting,
		Lines:          resolved,
	}, ref.ID, ref.Type, time.Now().UTC(), side, &repository.AuditInfo{
		Actor:     "system:" + ref.Source,
		EventType: "ledger.booked",
		Message:   describeDeferral(description, window),
	})
}
