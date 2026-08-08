package ledger

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/repository"
)

// Payroll posting account codes. Seeded by migration 017_payroll_gl.sql.
const (
	acctSalaryExpense    = "5200"
	acctPAYEPayable      = "2200"
	acctNSSFPayable      = "2210"
	acctNetSalaryPayable = "2220"
	acctOtherDeductions  = "2230"
)

var ErrPayrollUnbalanced = errors.New("payroll run does not balance: gross must equal deductions + net")

// PayrollRunInput is a finalized payroll run to post to the GL.
type PayrollRunInput struct {
	RunRef          string
	Period          string // YYYY-MM
	Gross           decimal.Decimal
	PAYE            decimal.Decimal
	NSSF            decimal.Decimal
	OtherDeductions decimal.Decimal
	Net             decimal.Decimal
	Currency        string
}

// PostPayrollRun books a finalized payroll run to the general ledger:
//
//	Dr  Salary & Wages Expense   gross
//	  Cr  PAYE Payable                  paye
//	  Cr  NSSF Payable                  nssf
//	  Cr  Other Payroll Deductions      other
//	  Cr  Net Salaries Payable          net
//
// It is idempotent on RunRef: posting the same run twice returns the existing
// record. Posting respects the fiscal-period close control, so a run cannot be
// booked into a closed period.
func (s *Service) PostPayrollRun(ctx context.Context, in PayrollRunInput) (*repository.PayrollRun, error) {
	if in.RunRef == "" || in.Period == "" {
		return nil, errors.New("payroll run requires runRef and period")
	}
	// Balance check: gross = paye + nssf + other + net, and gross > 0.
	deductionsPlusNet := in.PAYE.Add(in.NSSF).Add(in.OtherDeductions).Add(in.Net)
	if in.Gross.LessThanOrEqual(decimal.Zero) || !in.Gross.Equal(deductionsPlusNet) {
		return nil, ErrPayrollUnbalanced
	}

	// Idempotency: a run is posted exactly once.
	if existing, err := s.repo.GetPayrollRunByRef(ctx, in.RunRef); err != nil {
		return nil, err
	} else if existing != nil {
		return existing, nil
	}

	currency := in.Currency
	if currency == "" {
		currency = "UGX"
	}

	lines := []LineInput{
		{AccountCode: acctSalaryExpense, Debit: in.Gross, Memo: "Gross salary"},
		{AccountCode: acctNetSalaryPayable, Credit: in.Net, Memo: "Net pay"},
	}
	if in.PAYE.GreaterThan(decimal.Zero) {
		lines = append(lines, LineInput{AccountCode: acctPAYEPayable, Credit: in.PAYE, Memo: "PAYE withheld"})
	}
	if in.NSSF.GreaterThan(decimal.Zero) {
		lines = append(lines, LineInput{AccountCode: acctNSSFPayable, Credit: in.NSSF, Memo: "NSSF contribution"})
	}
	if in.OtherDeductions.GreaterThan(decimal.Zero) {
		lines = append(lines, LineInput{AccountCode: acctOtherDeductions, Credit: in.OtherDeductions, Memo: "Other deductions"})
	}

	sourceEventID := "payroll:" + in.RunRef
	sourceService := "payroll"
	entry, err := s.CreateJournalEntry(ctx, CreateEntryInput{
		Description:   "Payroll run " + in.RunRef + " (" + in.Period + ")",
		Lines:         lines,
		SourceEventID: &sourceEventID,
		SourceService: &sourceService,
	})
	if err != nil {
		return nil, err
	}
	posted, err := s.PostJournalEntry(ctx, entry.ID, "system:payroll")
	if err != nil {
		return nil, err
	}

	return s.repo.RecordPayrollRun(ctx, repository.PayrollRunParams{
		RunRef:          in.RunRef,
		Period:          in.Period,
		Gross:           in.Gross.StringFixed(2),
		PAYE:            in.PAYE.StringFixed(2),
		NSSF:            in.NSSF.StringFixed(2),
		OtherDeductions: in.OtherDeductions.StringFixed(2),
		Net:             in.Net.StringFixed(2),
		Currency:        currency,
		JournalEntryID:  posted.ID,
	})
}

// ListPayrollRuns returns posted payroll runs, newest first.
func (s *Service) ListPayrollRuns(ctx context.Context, limit int) ([]repository.PayrollRun, error) {
	return s.repo.ListPayrollRuns(ctx, limit)
}

// acctAccruedLeave is the liability for leave earned and not yet taken.
const acctAccruedLeave = "2240"

// ErrLeaveProvisionZero rejects a provision that would book nothing.
var ErrLeaveProvisionZero = errors.New("leave provision amount must not be zero")

// LeaveProvisionInput is a stated movement in the accrued-leave liability.
type LeaveProvisionInput struct {
	ProvisionRef string
	Period       string // YYYY-MM
	// Amount is the change in the liability, signed: positive accrues more
	// leave than was taken, negative means leave was consumed faster than it
	// was earned. It is deliberately not a closing balance — booking a balance
	// would re-post the whole obligation every period it did not move.
	Amount   decimal.Decimal
	Currency string
	Note     string
}

// PostLeaveProvision books a movement in the accrued-leave liability:
//
//	increase → Dr Salary & Wages Expense / Cr Accrued Leave
//	decrease → Dr Accrued Leave          / Cr Salary & Wages Expense
//
// The expense side is the account payroll already uses, so staff cost stays on
// one line rather than being split for no reporting benefit.
//
// This is an operator assertion, like the payroll totals beside it: HR reports
// leave taken but never the balance earned, and no service holds a pay rate to
// value a day with, so the platform cannot compute this figure yet. Whoever
// calculates payroll states it, and provision_ref makes the statement idempotent.
//
// Posting respects the fiscal-period close control, so a provision cannot be
// booked into a closed period.
func (s *Service) PostLeaveProvision(ctx context.Context, in LeaveProvisionInput) (*repository.LeaveProvision, error) {
	if in.ProvisionRef == "" || in.Period == "" {
		return nil, errors.New("leave provision requires provisionRef and period")
	}
	if in.Amount.IsZero() {
		return nil, ErrLeaveProvisionZero
	}

	// Idempotency: a provision is booked exactly once. A repeat returns the
	// original rather than doubling the liability.
	if existing, err := s.repo.GetLeaveProvisionByRef(ctx, in.ProvisionRef); err != nil {
		return nil, err
	} else if existing != nil {
		return existing, nil
	}

	currency := in.Currency
	if currency == "" {
		currency = "UGX"
	}

	amt := in.Amount.Abs()
	memo := "Accrued leave " + in.Period
	var lines []LineInput
	if in.Amount.IsPositive() {
		lines = []LineInput{
			{AccountCode: acctSalaryExpense, Debit: amt, Memo: memo},
			{AccountCode: acctAccruedLeave, Credit: amt, Memo: memo},
		}
	} else {
		lines = []LineInput{
			{AccountCode: acctAccruedLeave, Debit: amt, Memo: memo},
			{AccountCode: acctSalaryExpense, Credit: amt, Memo: memo},
		}
	}

	sourceEventID := "leave-provision:" + in.ProvisionRef
	sourceService := "payroll"
	entry, err := s.CreateJournalEntry(ctx, CreateEntryInput{
		Description:   "Leave provision " + in.ProvisionRef + " (" + in.Period + ")",
		Lines:         lines,
		SourceEventID: &sourceEventID,
		SourceService: &sourceService,
	})
	if err != nil {
		return nil, err
	}
	posted, err := s.PostJournalEntry(ctx, entry.ID, "system:payroll")
	if err != nil {
		return nil, err
	}

	return s.repo.RecordLeaveProvision(ctx, repository.LeaveProvisionParams{
		ProvisionRef:   in.ProvisionRef,
		Period:         in.Period,
		Amount:         in.Amount.StringFixed(2),
		Currency:       currency,
		Note:           in.Note,
		JournalEntryID: posted.ID,
	})
}

// ListLeaveProvisions returns booked provisions, newest first.
func (s *Service) ListLeaveProvisions(ctx context.Context, period string, limit int) ([]repository.LeaveProvision, error) {
	return s.repo.ListLeaveProvisions(ctx, period, limit)
}

// LeaveValuation is the outcome of measuring the accrued-leave liability.
type LeaveValuation struct {
	ValuedAt         time.Time       `json:"valuedAt"`
	TotalLiability   decimal.Decimal `json:"totalLiability"`
	Movement         decimal.Decimal `json:"movement"`
	EmployeesValued  int             `json:"employeesValued"`
	EmployeesUnrated int             `json:"employeesUnrated"`
	JournalEntryID   *uuid.UUID      `json:"journalEntryId,omitempty"`
}

// ValueLeaveLiability measures the obligation from the balances and rates HR
// reports, and books the movement since the last valuation.
//
// It books the *difference*, never the total: the liability is a standing
// balance, and posting it whole each period would accumulate the same
// obligation over and over. When the measured total has not moved, nothing is
// posted and the valuation is still recorded, so a period with no change is
// distinguishable from a period nobody valued.
//
// Employees with a balance and no rate are reported rather than dropped. Their
// leave is owed and unmeasured, and a total that quietly excludes them would
// read as complete.
//
// A date is valued once. Re-running one returns what was booked rather than
// measuring again: the movement was posted against the total as it stood, and
// the ledger will not accept a second entry for the same date. A balance
// corrected afterwards is not backdated — it flows through the next valuation's
// movement, which is how a standing accrual is meant to behave.
func (s *Service) ValueLeaveLiability(ctx context.Context, year int, valuedAt time.Time) (*LeaveValuation, error) {
	if year <= 0 {
		year = valuedAt.Year()
	}
	day := valuedAt.UTC().Truncate(24 * time.Hour)

	if booked, err := s.repo.GetLeaveValuation(ctx, day); err != nil {
		return nil, err
	} else if booked != nil {
		return leaveValuationFromRow(booked)
	}

	measured, err := s.repo.MeasureLeaveLiability(ctx, year)
	if err != nil {
		return nil, err
	}
	previous, err := s.repo.LastLeaveValuationTotal(ctx)
	if err != nil {
		return nil, err
	}

	out := &LeaveValuation{
		ValuedAt:         day,
		TotalLiability:   measured.Total,
		Movement:         measured.Total.Sub(previous),
		EmployeesValued:  measured.EmployeesValued,
		EmployeesUnrated: measured.EmployeesUnrated,
	}

	// The reporting currency is the ledger's own, not a constant. This was
	// hardcoded to UGX, which silently mislabels the liability on any deployment
	// whose books are kept in something else.
	params := repository.LeaveValuationParams{
		ValuedAt:         out.ValuedAt,
		TotalLiability:   out.TotalLiability.StringFixed(2),
		Movement:         out.Movement.StringFixed(2),
		Currency:         s.repo.BaseCurrency(),
		EmployeesValued:  out.EmployeesValued,
		EmployeesUnrated: out.EmployeesUnrated,
	}

	if out.Movement.IsZero() {
		// Nothing to post, but the date is still recorded so a period with no
		// change stays distinguishable from one nobody valued.
		if err := s.repo.RecordLeaveValuation(ctx, params); err != nil {
			return nil, err
		}
		return out, nil
	}

	amt := out.Movement.Abs()
	memo := "Accrued leave valuation " + out.ValuedAt.Format("2006-01-02")
	var lines []LineInput
	if out.Movement.IsPositive() {
		lines = []LineInput{
			{AccountCode: acctSalaryExpense, Debit: amt, Memo: memo},
			{AccountCode: acctAccruedLeave, Credit: amt, Memo: memo},
		}
	} else {
		lines = []LineInput{
			{AccountCode: acctAccruedLeave, Debit: amt, Memo: memo},
			{AccountCode: acctSalaryExpense, Credit: amt, Memo: memo},
		}
	}
	resolved, err := s.resolveLines(ctx, lines)
	if err != nil {
		return nil, err
	}
	// A valuation posts on its own date, which may be backdated, so the closed
	// period has to be checked against that date rather than today.
	if closed, err := s.repo.IsPeriodClosed(ctx, out.ValuedAt.Format("2006-01")); err != nil {
		return nil, err
	} else if closed {
		return nil, ErrPeriodClosed
	}

	sourceEventID := "leave-valuation:" + out.ValuedAt.Format("2006-01-02")
	sourceService := "payroll"
	// The valuation record is written inside the journal's transaction, so the
	// posting and the record of it commit together or not at all. Splitting them
	// left the ledger carrying a movement the valuation table had no row for,
	// and the next run then measured against a stale previous total.
	side := func(ctx context.Context, tx pgx.Tx, entryID uuid.UUID) error {
		p := params
		p.JournalEntryID = &entryID
		return repository.RecordLeaveValuationTx(ctx, tx, p)
	}
	entry, err := s.repo.BookPostedEntry(ctx, repository.CreateJournalParams{
		Description:   memo,
		SourceEventID: &sourceEventID,
		SourceService: &sourceService,
		Currency:      params.Currency,
		Lines:         resolved,
	}, sourceEventID, "payroll.leave.valued", out.ValuedAt, side, &repository.AuditInfo{
		Actor:     "system:payroll",
		EventType: "ledger.leave.valued",
		Message:   memo,
	})
	if err != nil {
		return nil, err
	}
	out.JournalEntryID = &entry.ID
	return out, nil
}

// leaveValuationFromRow rebuilds a valuation from the row that was booked, so a
// repeated request for a valued date answers with what is in the ledger instead
// of a fresh measurement that could not be posted anyway.
func leaveValuationFromRow(row *repository.LeaveValuationRow) (*LeaveValuation, error) {
	total, err := decimal.NewFromString(row.TotalLiability)
	if err != nil {
		return nil, err
	}
	movement, err := decimal.NewFromString(row.Movement)
	if err != nil {
		return nil, err
	}
	return &LeaveValuation{
		ValuedAt:         row.ValuedAt,
		TotalLiability:   total,
		Movement:         movement,
		EmployeesValued:  row.EmployeesValued,
		EmployeesUnrated: row.EmployeesUnrated,
		JournalEntryID:   row.JournalEntryID,
	}, nil
}
