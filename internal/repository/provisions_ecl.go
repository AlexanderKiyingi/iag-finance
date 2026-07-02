package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/domain"
)

// IFRS 9 expected-credit-loss provisioning, write-off and recovery.
//
// The allowance (1190) is a credit-normal contra-asset: a positive balance
// reduces net AR on the balance sheet. Provisioning books only the MOVEMENT
// from the current allowance to the target (so re-running keeps the allowance at
// target rather than double-counting). Write-off consumes the allowance first
// and expenses only the uncovered remainder; recovery of a written-off debt is
// income (4300), never a reversal of the original sale.

const (
	allowanceCode  = "1190"
	badDebtExpense = "5400"
	badDebtRecov   = "4300"
	arControlCode  = "1100"
	cashCode       = "1000"
)

// ECLProvision is the outcome of a period provisioning run.
type ECLProvision struct {
	Period         string          `json:"period"`
	Target         decimal.Decimal `json:"target"`
	Movement       decimal.Decimal `json:"movement"`
	JournalEntryID *uuid.UUID      `json:"journalEntryId,omitempty"`
}

// ARWriteOff records a formal receivable write-off.
type ARWriteOff struct {
	DocumentRef        string          `json:"documentRef"`
	CustomerRef        string          `json:"customerRef"`
	Amount             decimal.Decimal `json:"amount"`
	CoveredByAllowance decimal.Decimal `json:"coveredByAllowance"`
	Currency           string          `json:"currency"`
	JournalEntryID     *uuid.UUID      `json:"journalEntryId,omitempty"`
}

// ErrNothingToWriteOff indicates the AR item has no open balance to write off.
var ErrNothingToWriteOff = errors.New("receivable has no open balance to write off")

// ErrWriteOffNotFound indicates no prior write-off exists for a recovery.
var ErrWriteOffNotFound = errors.New("no write-off found for document")

// eclTarget computes the target allowance (base currency) as the sum over open
// AR of outstanding × document-rate × the bucket loss rate.
func (r *Repository) eclTarget(ctx context.Context) (decimal.Decimal, error) {
	var s string
	err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM((a.amount - a.amount_paid) * a.fx_rate * r.loss_rate), 0)::text
		FROM ar_open_items a
		JOIN ecl_rates r ON r.bucket = (CASE
			WHEN a.due_date IS NULL OR a.due_date >= CURRENT_DATE THEN 'current'
			WHEN a.due_date >= CURRENT_DATE - INTERVAL '30 days' THEN '1-30'
			WHEN a.due_date >= CURRENT_DATE - INTERVAL '60 days' THEN '31-60'
			WHEN a.due_date >= CURRENT_DATE - INTERVAL '90 days' THEN '61-90'
			ELSE '90+'
		END)
		WHERE a.status IN ('open', 'partial')
	`).Scan(&s)
	if err != nil {
		return decimal.Zero, err
	}
	d, _ := decimal.NewFromString(s)
	return d, nil
}

// allowanceBalanceBase returns the current allowance (1190) balance in base
// currency, credit-normal (credit − debit).
func (r *Repository) allowanceBalanceBase(ctx context.Context) (decimal.Decimal, error) {
	var s string
	err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(jl.credit_base - jl.debit_base), 0)::text
		FROM journal_lines jl
		JOIN journal_entries je ON je.id = jl.journal_entry_id AND je.status = 'posted'
		JOIN chart_of_accounts coa ON coa.id = jl.account_id
		WHERE coa.code = $1
	`, allowanceCode).Scan(&s)
	if err != nil {
		return decimal.Zero, err
	}
	d, _ := decimal.NewFromString(s)
	return d, nil
}

// accountIDByCode resolves an active account id outside a transaction.
func (r *Repository) accountIDByCode(ctx context.Context, code string) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(ctx, `SELECT id FROM chart_of_accounts WHERE code = $1 AND active = TRUE`, code).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrAccountNotFoundRepo(code)
	}
	return id, err
}

// ErrAccountNotFoundRepo builds a not-found error for a missing GL account.
func ErrAccountNotFoundRepo(code string) error {
	return errors.New("account not found: " + code)
}

// BookECLProvision books the movement to the target allowance for a period and
// records the run. Idempotent on both the source event (ecl:<period>) and the
// unique period in ar_provisions.
func (r *Repository) BookECLProvision(ctx context.Context, period string, postedAt time.Time, audit *AuditInfo) (*ECLProvision, error) {
	target, err := r.eclTarget(ctx)
	if err != nil {
		return nil, err
	}
	current, err := r.allowanceBalanceBase(ctx)
	if err != nil {
		return nil, err
	}
	movement := target.Sub(current)
	out := &ECLProvision{Period: period, Target: target, Movement: movement}

	if movement.IsZero() {
		// No GL movement — record the run (idempotent) with no entry.
		if _, err := r.pool.Exec(ctx, `
			INSERT INTO ar_provisions (period, computed_amount, movement)
			VALUES ($1, $2::numeric, 0) ON CONFLICT (period) DO NOTHING
		`, period, target.String()); err != nil {
			return nil, err
		}
		return out, nil
	}

	expenseID, err := r.accountIDByCode(ctx, badDebtExpense)
	if err != nil {
		return nil, err
	}
	allowID, err := r.accountIDByCode(ctx, allowanceCode)
	if err != nil {
		return nil, err
	}

	var lines []ResolvedLine
	if movement.IsPositive() {
		// Increase allowance: Dr Bad Debt Expense / Cr Allowance.
		lines = []ResolvedLine{
			{AccountID: expenseID, Debit: movement, Memo: "ECL provision " + period, LineOrder: 0},
			{AccountID: allowID, Credit: movement, Memo: "Allowance for doubtful debts", LineOrder: 1},
		}
	} else {
		// Release allowance: Dr Allowance / Cr Bad Debt Expense.
		amt := movement.Neg()
		lines = []ResolvedLine{
			{AccountID: allowID, Debit: amt, Memo: "ECL release " + period, LineOrder: 0},
			{AccountID: expenseID, Credit: amt, Memo: "Bad debt expense release", LineOrder: 1},
		}
	}

	eventID := "ecl:" + period
	src := "iag.finance"
	entry, err := r.BookPostedEntry(ctx, CreateJournalParams{
		Description:    "ECL provision " + period,
		SourceEventID:  &eventID,
		SourceService:  &src,
		AccountingDate: postedAt,
		Lines:          lines,
	}, eventID, "finance.ecl.provision", postedAt, func(ctx context.Context, tx pgx.Tx, entryID uuid.UUID) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO ar_provisions (period, computed_amount, movement, journal_entry_id)
			VALUES ($1, $2::numeric, $3::numeric, $4)
			ON CONFLICT (period) DO NOTHING
		`, period, target.String(), movement.String(), entryID)
		return err
	}, audit)
	if err != nil {
		return nil, err
	}
	out.JournalEntryID = &entry.ID
	return out, nil
}

// WriteOffReceivable de-recognises an open receivable: it consumes the allowance
// first (Dr 1190) and expenses any uncovered remainder (Dr 5400), crediting AR
// (1100). It closes the open item and records the write-off. Idempotent on
// ar-writeoff:<documentRef>.
func (r *Repository) WriteOffReceivable(ctx context.Context, documentRef, reason string, postedAt time.Time, audit *AuditInfo) (*ARWriteOff, error) {
	item, err := r.GetARByDocumentRef(ctx, documentRef)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, ErrOriginalNotFound
	}
	amount, _ := decimal.NewFromString(item.Amount)
	paid, _ := decimal.NewFromString(item.AmountPaid)
	outstanding := amount.Sub(paid)
	if outstanding.LessThanOrEqual(decimal.Zero) {
		return nil, ErrNothingToWriteOff
	}
	fxRate, err := r.OpenItemFXRateByDocRef(ctx, "ar", documentRef)
	if err != nil {
		return nil, err
	}
	if fxRate.IsZero() {
		fxRate = decimal.NewFromInt(1)
	}
	allowance, err := r.allowanceBalanceBase(ctx)
	if err != nil {
		return nil, err
	}
	if allowance.IsNegative() {
		allowance = decimal.Zero
	}
	outstandingBase := outstanding.Mul(fxRate)
	coveredBase := outstandingBase
	if allowance.LessThan(coveredBase) {
		coveredBase = allowance
	}
	coveredDoc := coveredBase.DivRound(fxRate, 2)
	if coveredDoc.GreaterThan(outstanding) {
		coveredDoc = outstanding
	}
	uncoveredDoc := outstanding.Sub(coveredDoc)

	allowID, err := r.accountIDByCode(ctx, allowanceCode)
	if err != nil {
		return nil, err
	}
	expenseID, err := r.accountIDByCode(ctx, badDebtExpense)
	if err != nil {
		return nil, err
	}
	arID, err := r.accountIDByCode(ctx, arControlCode)
	if err != nil {
		return nil, err
	}

	lines := make([]ResolvedLine, 0, 3)
	order := 0
	if coveredDoc.IsPositive() {
		lines = append(lines, ResolvedLine{AccountID: allowID, Debit: coveredDoc, Memo: "Write-off against allowance " + documentRef, LineOrder: order})
		order++
	}
	if uncoveredDoc.IsPositive() {
		lines = append(lines, ResolvedLine{AccountID: expenseID, Debit: uncoveredDoc, Memo: "Write-off to bad debt expense " + documentRef, LineOrder: order})
		order++
	}
	lines = append(lines, ResolvedLine{AccountID: arID, Credit: outstanding, Memo: "De-recognise receivable " + documentRef, LineOrder: order})

	eventID := "ar-writeoff:" + documentRef
	src := "iag.finance"
	out := &ARWriteOff{
		DocumentRef: documentRef, CustomerRef: item.CustomerRef, Amount: outstanding,
		CoveredByAllowance: coveredDoc, Currency: item.Currency,
	}
	entry, err := r.BookPostedEntry(ctx, CreateJournalParams{
		Description:    "Write-off " + documentRef,
		SourceEventID:  &eventID,
		SourceService:  &src,
		Currency:       item.Currency,
		FXRate:         fxRate,
		AccountingDate: postedAt,
		Lines:          lines,
	}, eventID, "finance.ar.writeoff", postedAt, func(ctx context.Context, tx pgx.Tx, entryID uuid.UUID) error {
		if _, err := tx.Exec(ctx, `
			UPDATE ar_open_items SET status = 'closed', updated_at = NOW()
			WHERE document_ref = $1 AND status != 'closed'
		`, documentRef); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO ar_writeoffs (document_ref, customer_ref, amount, covered_by_allowance, currency, reason, journal_entry_id)
			VALUES ($1, $2, $3::numeric, $4::numeric, $5, $6, $7)
			ON CONFLICT (document_ref) DO NOTHING
		`, documentRef, item.CustomerRef, outstanding.String(), coveredDoc.String(), item.Currency, reason, entryID)
		return err
	}, audit)
	if err != nil {
		return nil, err
	}
	out.JournalEntryID = &entry.ID
	return out, nil
}

// RecoverWrittenOff books cash recovered on a previously written-off debt as
// income (Dr Cash / Cr Bad Debt Recovery) and accrues it against the write-off.
// Idempotent on ar-recovery:<reference>.
func (r *Repository) RecoverWrittenOff(ctx context.Context, documentRef, reference string, amount decimal.Decimal, postedAt time.Time, audit *AuditInfo) (*domain.JournalEntry, error) {
	if amount.LessThanOrEqual(decimal.Zero) {
		return nil, ErrNothingToWriteOff
	}
	// The write-off must exist.
	var exists bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ar_writeoffs WHERE document_ref = $1)`, documentRef).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrWriteOffNotFound
	}
	cashID, err := r.accountIDByCode(ctx, cashCode)
	if err != nil {
		return nil, err
	}
	recovID, err := r.accountIDByCode(ctx, badDebtRecov)
	if err != nil {
		return nil, err
	}
	lines := []ResolvedLine{
		{AccountID: cashID, Debit: amount, Memo: "Recovery of written-off debt " + documentRef, LineOrder: 0},
		{AccountID: recovID, Credit: amount, Memo: "Bad debt recovery income", LineOrder: 1},
	}
	eventID := "ar-recovery:" + reference
	src := "iag.finance"
	entry, err := r.BookPostedEntry(ctx, CreateJournalParams{
		Description:    "Bad debt recovery " + documentRef,
		SourceEventID:  &eventID,
		SourceService:  &src,
		AccountingDate: postedAt,
		Lines:          lines,
	}, eventID, "finance.ar.recovery", postedAt, func(ctx context.Context, tx pgx.Tx, entryID uuid.UUID) error {
		_, err := tx.Exec(ctx, `
			UPDATE ar_writeoffs SET recovered_amount = recovered_amount + $2::numeric
			WHERE document_ref = $1
		`, documentRef, amount.String())
		return err
	}, audit)
	if err != nil {
		return nil, err
	}
	return entry, nil
}

// ListECLProvisions returns recent provisioning runs, newest first.
func (r *Repository) ListECLProvisions(ctx context.Context, limit int) ([]ECLProvision, error) {
	if limit <= 0 || limit > 200 {
		limit = 60
	}
	rows, err := r.pool.Query(ctx, `
		SELECT period, computed_amount::text, movement::text, journal_entry_id
		FROM ar_provisions ORDER BY period DESC LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ECLProvision
	for rows.Next() {
		var p ECLProvision
		var target, movement string
		if err := rows.Scan(&p.Period, &target, &movement, &p.JournalEntryID); err != nil {
			return nil, err
		}
		p.Target, _ = decimal.NewFromString(target)
		p.Movement, _ = decimal.NewFromString(movement)
		out = append(out, p)
	}
	return out, rows.Err()
}
