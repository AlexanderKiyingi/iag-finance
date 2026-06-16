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

// retainedEarningsCode is the equity account that absorbs each year's net
// income/loss at close (seeded in the default chart of accounts).
const retainedEarningsCode = "3000"

// ErrYearHasDrafts blocks a year-end close while unposted entries are dated in
// the year — they must be posted or deleted first.
var ErrYearHasDrafts = errors.New("year has draft entries; post or delete them before closing the year")

// ErrNothingToClose indicates the year had no revenue/expense activity to close.
var ErrNothingToClose = errors.New("no revenue or expense activity to close for the year")

// CloseFiscalYear posts the year-end closing entry — it zeroes every revenue and
// expense account for the calendar year into Retained Earnings (3000) — then
// locks the year by closing all twelve monthly fiscal periods. It is idempotent:
// the closing entry is keyed on a synthetic event id, so a second call returns
// the existing entry instead of double-closing, and the period locks upsert.
// The closing entry is dated 31 Dec of the year, so post-close the P&L for the
// year nets to zero and the profit lives in Retained Earnings — the balance
// sheet's computed "Current Period Earnings" line then falls to zero.
func (s *Service) CloseFiscalYear(ctx context.Context, year int, actor string, by *uuid.UUID) (*domain.JournalEntry, error) {
	drafts, err := s.repo.CountDraftEntriesInYear(ctx, year)
	if err != nil {
		return nil, err
	}
	if drafts > 0 {
		return nil, ErrYearHasDrafts
	}

	balances, err := s.repo.RevenueExpenseBalancesForYear(ctx, year)
	if err != nil {
		return nil, err
	}

	lines := make([]LineInput, 0, len(balances)+1)
	netIncome := decimal.Zero
	for _, b := range balances {
		if b.Balance.IsZero() {
			continue
		}
		switch b.Type {
		case "revenue":
			// Revenue is credit-normal: debit its balance to zero it.
			lines = append(lines, zeroingLine(b.Code, b.Balance, true))
			netIncome = netIncome.Add(b.Balance)
		case "expense":
			// Expense is debit-normal: credit its balance to zero it.
			lines = append(lines, zeroingLine(b.Code, b.Balance, false))
			netIncome = netIncome.Sub(b.Balance)
		}
	}
	if len(lines) == 0 {
		return nil, ErrNothingToClose
	}

	// Balance the entry to Retained Earnings: a profit credits equity, a loss debits it.
	switch {
	case netIncome.IsPositive():
		lines = append(lines, LineInput{AccountCode: retainedEarningsCode, Credit: netIncome, Memo: "Net income to retained earnings"})
	case netIncome.IsNegative():
		lines = append(lines, LineInput{AccountCode: retainedEarningsCode, Debit: netIncome.Neg(), Memo: "Net loss to retained earnings"})
	}

	if err := validateBalance(lines); err != nil {
		return nil, err
	}
	resolved, err := s.resolveLines(ctx, lines)
	if err != nil {
		return nil, err
	}

	eventID := fmt.Sprintf("yearend-close:%d", year)
	accountingDate := time.Date(year, 12, 31, 0, 0, 0, 0, time.UTC)
	entry, err := s.repo.BookPostedEntry(ctx, repository.CreateJournalParams{
		Description:    fmt.Sprintf("Year-end close %d", year),
		SourceEventID:  &eventID,
		SourceService:  optionalString("iag.finance"),
		AccountingDate: accountingDate,
		Lines:          resolved,
	}, eventID, "finance.yearend.close", time.Now().UTC(), nil, &repository.AuditInfo{
		Actor:     actorOrSystem(actor),
		EventType: "ledger.yearend.close",
		Message:   fmt.Sprintf("year-end close %d: net income %s", year, netIncome.String()),
	})
	if err != nil {
		return nil, err
	}

	// Lock the year — close every month so nothing can be posted back into it.
	for m := 1; m <= 12; m++ {
		period := fmt.Sprintf("%04d-%02d", year, m)
		if err := s.repo.SetPeriodStatus(ctx, period, "closed", by); err != nil {
			return nil, err
		}
	}
	return entry, nil
}

// zeroingLine builds the closing line that drives an account's balance to zero.
// debitToClear is true for credit-normal accounts (revenue) and false for
// debit-normal accounts (expense); a negative balance flips the side so contra
// balances also close correctly.
func zeroingLine(code string, balance decimal.Decimal, debitToClear bool) LineInput {
	if balance.IsNegative() {
		debitToClear = !debitToClear
		balance = balance.Neg()
	}
	if debitToClear {
		return LineInput{AccountCode: code, Debit: balance, Memo: "Year-end close"}
	}
	return LineInput{AccountCode: code, Credit: balance, Memo: "Year-end close"}
}
