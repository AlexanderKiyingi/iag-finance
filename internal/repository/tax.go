package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/domain"
)

const (
	inputVATCode  = "1300"
	outputVATCode = "2100"
)

// ErrNotReverseCharge indicates the tax code is not a reverse-charge code (or has
// no VAT to self-assess).
var ErrNotReverseCharge = errors.New("tax code is not a reverse-charge code")

// TaxCode is a configurable VAT/GST rate. The GL account is chosen by direction
// at booking time (sales → output VAT 2100, purchases → input VAT 1300); the code
// supplies the rate.
type TaxCode struct {
	Code          string `json:"code"`
	Name          string `json:"name"`
	Rate          string `json:"rate"`
	Active        bool   `json:"active"`
	ReverseCharge bool   `json:"reverseCharge"`
}

func (r *Repository) ListTaxCodes(ctx context.Context) ([]TaxCode, error) {
	rows, err := r.pool.Query(ctx, `SELECT code, name, rate::text, active, reverse_charge FROM tax_codes ORDER BY code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TaxCode
	for rows.Next() {
		var t TaxCode
		if err := rows.Scan(&t.Code, &t.Name, &t.Rate, &t.Active, &t.ReverseCharge); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetTaxCode returns an active tax code's rate and reverse-charge flag.
func (r *Repository) GetTaxCode(ctx context.Context, code string) (decimal.Decimal, bool, bool, error) {
	var rateStr string
	var reverseCharge bool
	err := r.pool.QueryRow(ctx, `SELECT rate::text, reverse_charge FROM tax_codes WHERE code = $1 AND active = TRUE`, code).Scan(&rateStr, &reverseCharge)
	if err != nil {
		if err == pgx.ErrNoRows {
			return decimal.Zero, false, false, nil
		}
		return decimal.Zero, false, false, err
	}
	d, err := decimal.NewFromString(rateStr)
	return d, reverseCharge, true, err
}

// SelfAssessReverseCharge books the buyer's reverse-charge VAT self-assessment on
// a net purchase: Dr Input VAT (1300) / Cr Output VAT (2100) for net × rate — a
// net-zero cash effect that recognises both sides. Idempotent on
// reverse-charge:<reference>.
func (r *Repository) SelfAssessReverseCharge(ctx context.Context, code, reference string, net decimal.Decimal, postedAt time.Time, audit *AuditInfo) (*domain.JournalEntry, error) {
	rate, reverseCharge, ok, err := r.GetTaxCode(ctx, code)
	if err != nil {
		return nil, err
	}
	if !ok || !reverseCharge {
		return nil, ErrNotReverseCharge
	}
	vat := net.Mul(rate).Round(2)
	if vat.LessThanOrEqual(decimal.Zero) {
		return nil, ErrNotReverseCharge
	}
	inputID, err := r.accountIDByCode(ctx, inputVATCode)
	if err != nil {
		return nil, err
	}
	outputID, err := r.accountIDByCode(ctx, outputVATCode)
	if err != nil {
		return nil, err
	}
	lines := []ResolvedLine{
		{AccountID: inputID, Debit: vat, Memo: "Reverse-charge input VAT", LineOrder: 0},
		{AccountID: outputID, Credit: vat, Memo: "Reverse-charge output VAT", LineOrder: 1},
	}
	eventID := "reverse-charge:" + reference
	src := "iag.finance"
	return r.BookPostedEntry(ctx, CreateJournalParams{
		Description:    "Reverse-charge VAT self-assessment " + reference,
		SourceEventID:  &eventID,
		SourceService:  &src,
		AccountingDate: postedAt,
		Lines:          lines,
	}, eventID, "finance.vat.reverse_charge", postedAt, nil, audit)
}

// GetTaxRate returns the rate (e.g. 0.18) for an active tax code, or false if
// the code is unknown/inactive.
func (r *Repository) GetTaxRate(ctx context.Context, code string) (decimal.Decimal, bool, error) {
	var rateStr string
	err := r.pool.QueryRow(ctx, `SELECT rate::text FROM tax_codes WHERE code = $1 AND active = TRUE`, code).Scan(&rateStr)
	if err != nil {
		if err == pgx.ErrNoRows {
			return decimal.Zero, false, nil
		}
		return decimal.Zero, false, err
	}
	d, err := decimal.NewFromString(rateStr)
	return d, true, err
}

func (r *Repository) UpsertTaxCode(ctx context.Context, code, name, rate string, active, reverseCharge bool) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO tax_codes (code, name, rate, active, reverse_charge) VALUES ($1, $2, $3::numeric, $4, $5)
		ON CONFLICT (code) DO UPDATE SET name = EXCLUDED.name, rate = EXCLUDED.rate, active = EXCLUDED.active, reverse_charge = EXCLUDED.reverse_charge
	`, code, name, rate, active, reverseCharge)
	return err
}

// VATReturnReport summarises output vs input VAT for a period (base currency).
type VATReturnReport struct {
	OutputVAT  string `json:"outputVat"`
	InputVAT   string `json:"inputVat"`
	NetPayable string `json:"netPayable"` // output − input (positive = owed to URA)
}

// VATReturn aggregates posted output VAT (2100) and recoverable input VAT (1300)
// over [from, to] and nets them. Amounts are base currency.
func (r *Repository) VATReturn(ctx context.Context, from, to *time.Time) (VATReturnReport, error) {
	var outStr, inStr string
	err := r.pool.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(jl.credit_base - jl.debit_base) FILTER (WHERE coa.code = '2100'), 0)::text,
			COALESCE(SUM(jl.debit_base - jl.credit_base) FILTER (WHERE coa.code = '1300'), 0)::text
		FROM journal_lines jl
		JOIN chart_of_accounts coa ON coa.id = jl.account_id
		JOIN journal_entries je ON je.id = jl.journal_entry_id AND je.status = 'posted'
			AND ($1::date IS NULL OR je.accounting_date >= $1)
			AND ($2::date IS NULL OR je.accounting_date <= $2)
		WHERE coa.code IN ('2100', '1300')
	`, from, to).Scan(&outStr, &inStr)
	if err != nil {
		return VATReturnReport{}, err
	}
	outVAT, _ := decimal.NewFromString(outStr)
	inVAT, _ := decimal.NewFromString(inStr)
	return VATReturnReport{
		OutputVAT:  outVAT.StringFixed(2),
		InputVAT:   inVAT.StringFixed(2),
		NetPayable: outVAT.Sub(inVAT).StringFixed(2),
	}, nil
}
