package ledger

import (
	"context"
	"errors"

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
	posted, err := s.PostJournalEntry(ctx, entry.ID)
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
