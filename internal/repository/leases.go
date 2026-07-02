package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/domain"
)

// IFRS 16 leases — right-of-use asset + lease liability.
//
// Recognition computes the present value of the lease payments (the initial
// liability and ROU asset), books Dr 1600 ROU / Cr 2500 Lease Liability, and
// precomputes the full amortization schedule. A periodic lease run books, for
// every due un-booked line across all leases:
//   Dr 5600 Interest        (Σ interest, the liability unwind)
//   Dr 5320 ROU Depreciation(Σ straight-line depreciation)
//   Dr 2500 Lease Liability (Σ principal)
//   Cr 1000 Cash            (Σ payment)
//   Cr 1610 Accum Dep - ROU (Σ depreciation)
// which balances because payment = interest + principal.

const (
	rouAssetCode      = "1600"
	rouAccumDepCode   = "1610"
	leaseLiabCode     = "2500"
	rouDeprCode       = "5320"
	leaseInterestCode = "5600"
)

var (
	// ErrLeaseExists indicates a lease already exists for this ref.
	ErrLeaseExists = errors.New("lease already exists for reference")
	// ErrLeaseNotFound indicates the referenced lease is absent.
	ErrLeaseNotFound = errors.New("lease not found")
)

// Lease is a recognised IFRS 16 lease.
type Lease struct {
	ID               uuid.UUID       `json:"id"`
	LeaseRef         string          `json:"leaseRef"`
	Description      string          `json:"description"`
	Currency         string          `json:"currency"`
	MonthlyPayment   decimal.Decimal `json:"monthlyPayment"`
	AnnualRate       decimal.Decimal `json:"annualRate"`
	TermMonths       int             `json:"termMonths"`
	StartPeriod      string          `json:"startPeriod"`
	ROUAsset         decimal.Decimal `json:"rouAsset"`
	InitialLiability decimal.Decimal `json:"initialLiability"`
	Status           string          `json:"status"`
	CreatedAt        time.Time       `json:"createdAt"`
}

// CreateLeaseInput describes a new lease.
type CreateLeaseInput struct {
	LeaseRef       string
	Description    string
	Currency       string
	MonthlyPayment decimal.Decimal
	AnnualRate     decimal.Decimal // e.g. 0.12 for 12%/yr; 0 = interest-free
	TermMonths     int
	StartPeriod    string // YYYY-MM
}

type scheduleRow struct {
	period       string
	opening      decimal.Decimal
	interest     decimal.Decimal
	payment      decimal.Decimal
	principal    decimal.Decimal
	closing      decimal.Decimal
	depreciation decimal.Decimal
}

// buildLeaseSchedule computes the present value of the payments and the full
// amortization + straight-line depreciation schedule. The final period absorbs
// rounding so the liability closes at exactly zero and accumulated depreciation
// equals the ROU asset.
func buildLeaseSchedule(payment, annualRate decimal.Decimal, term int, startPeriod string) (pv decimal.Decimal, rows []scheduleRow) {
	monthly := annualRate.Div(decimal.NewFromInt(12))
	one := decimal.NewFromInt(1)

	// Present value = Σ payment / (1+r)^i for i=1..term.
	pv = decimal.Zero
	df := one // discount factor (1+r)^-i, built iteratively
	onePlusR := one.Add(monthly)
	for i := 0; i < term; i++ {
		if monthly.IsZero() {
			pv = pv.Add(payment)
		} else {
			df = df.DivRound(onePlusR, 12)
			pv = pv.Add(payment.Mul(df))
		}
	}
	pv = pv.Round(2)

	rou := pv
	deprPer := rou.DivRound(decimal.NewFromInt(int64(term)), 2)

	opening := pv
	accumDepr := decimal.Zero
	for i := 0; i < term; i++ {
		interest := opening.Mul(monthly).Round(2)
		principal := payment.Sub(interest)
		depr := deprPer
		if i == term-1 {
			// Last period: principal clears the remaining liability exactly; the
			// final depreciation absorbs any rounding so accum dep == ROU.
			principal = opening
			depr = rou.Sub(accumDepr)
		}
		closing := opening.Sub(principal)
		accumDepr = accumDepr.Add(depr)
		rows = append(rows, scheduleRow{
			period:       addMonths(startPeriod, i),
			opening:      opening,
			interest:     interest,
			payment:      interest.Add(principal),
			principal:    principal,
			closing:      closing,
			depreciation: depr,
		})
		opening = closing
	}
	return pv, rows
}

// CreateLease recognises a lease (Dr 1600 ROU / Cr 2500 Lease Liability at the
// present value) and stores its amortization schedule. Idempotent on
// lease:<leaseRef>.
func (r *Repository) CreateLease(ctx context.Context, in CreateLeaseInput, postedAt time.Time, audit *AuditInfo) (*Lease, error) {
	if in.MonthlyPayment.LessThanOrEqual(decimal.Zero) {
		return nil, errors.New("monthly payment must be positive")
	}
	if in.TermMonths < 1 {
		return nil, errors.New("term must be at least 1 month")
	}
	if in.AnnualRate.IsNegative() {
		return nil, errors.New("annual rate cannot be negative")
	}
	if in.Currency == "" {
		in.Currency = r.baseCurrency
	}
	if _, err := time.Parse("2006-01", in.StartPeriod); err != nil {
		return nil, fmt.Errorf("startPeriod must be YYYY-MM: %w", err)
	}

	pv, schedule := buildLeaseSchedule(in.MonthlyPayment, in.AnnualRate, in.TermMonths, in.StartPeriod)
	if pv.LessThanOrEqual(decimal.Zero) {
		return nil, errors.New("computed lease present value is not positive")
	}

	rouID, err := r.accountIDByCode(ctx, rouAssetCode)
	if err != nil {
		return nil, err
	}
	liabID, err := r.accountIDByCode(ctx, leaseLiabCode)
	if err != nil {
		return nil, err
	}

	lines := []ResolvedLine{
		{AccountID: rouID, Debit: pv, Memo: "Right-of-use asset " + in.LeaseRef, LineOrder: 0},
		{AccountID: liabID, Credit: pv, Memo: "Lease liability " + in.LeaseRef, LineOrder: 1},
	}
	rate := r.RateOrOne(ctx, in.Currency, postedAt)
	eventID := "lease:" + in.LeaseRef
	src := "iag.finance"

	var out *Lease
	_, err = r.BookPostedEntry(ctx, CreateJournalParams{
		Description:    "Recognise lease " + in.LeaseRef,
		SourceEventID:  &eventID,
		SourceService:  &src,
		Currency:       in.Currency,
		FXRate:         rate,
		AccountingDate: postedAt,
		Lines:          lines,
	}, eventID, "finance.lease.recognize", postedAt, func(ctx context.Context, tx pgx.Tx, entryID uuid.UUID) error {
		var leaseID uuid.UUID
		err := tx.QueryRow(ctx, `
			INSERT INTO leases (lease_ref, entity_id, description, currency, monthly_payment, annual_rate, term_months, start_period, rou_asset, initial_liability, recognize_entry_id)
			VALUES ($1, $2, $3, $4, $5::numeric, $6::numeric, $7, $8, $9::numeric, $9::numeric, $10)
			RETURNING id
		`, in.LeaseRef, EntityFromContext(ctx), in.Description, in.Currency, in.MonthlyPayment.String(), in.AnnualRate.String(), in.TermMonths, in.StartPeriod, pv.String(), entryID).Scan(&leaseID)
		if err != nil {
			if IsUniqueViolation(err) {
				return ErrLeaseExists
			}
			return err
		}
		for _, s := range schedule {
			if _, err := tx.Exec(ctx, `
				INSERT INTO lease_schedule_lines (lease_id, period, opening_liability, interest, payment, principal, closing_liability, depreciation)
				VALUES ($1, $2, $3::numeric, $4::numeric, $5::numeric, $6::numeric, $7::numeric, $8::numeric)
			`, leaseID, s.period, s.opening.String(), s.interest.String(), s.payment.String(), s.principal.String(), s.closing.String(), s.depreciation.String()); err != nil {
				return err
			}
		}
		out = &Lease{
			ID: leaseID, LeaseRef: in.LeaseRef, Description: in.Description, Currency: in.Currency,
			MonthlyPayment: in.MonthlyPayment, AnnualRate: in.AnnualRate, TermMonths: in.TermMonths,
			StartPeriod: in.StartPeriod, ROUAsset: pv, InitialLiability: pv, Status: "active",
		}
		return nil
	}, audit)
	if err != nil {
		return nil, err
	}
	if out == nil {
		return r.GetLeaseByRef(ctx, in.LeaseRef)
	}
	return out, nil
}

// RunLeasePeriod books interest, payment and depreciation for every due,
// un-booked lease line on/before the period, in one balanced entry. Idempotent
// on leaserun:<period> for the batch.
func (r *Repository) RunLeasePeriod(ctx context.Context, period string, postedAt time.Time, audit *AuditInfo) (*domain.JournalEntry, decimal.Decimal, int, error) {
	if _, err := time.Parse("2006-01", period); err != nil {
		return nil, decimal.Zero, 0, fmt.Errorf("period must be YYYY-MM: %w", err)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, interest::text, principal::text, payment::text, depreciation::text
		FROM lease_schedule_lines
		WHERE period <= $1 AND journal_entry_id IS NULL
		ORDER BY period, id
	`, period)
	if err != nil {
		return nil, decimal.Zero, 0, err
	}
	var dueIDs []uuid.UUID
	sumInterest, sumPrincipal, sumPayment, sumDepr := decimal.Zero, decimal.Zero, decimal.Zero, decimal.Zero
	for rows.Next() {
		var id uuid.UUID
		var it, pr, pm, dp string
		if err := rows.Scan(&id, &it, &pr, &pm, &dp); err != nil {
			rows.Close()
			return nil, decimal.Zero, 0, err
		}
		iv, _ := decimal.NewFromString(it)
		pv, _ := decimal.NewFromString(pr)
		mv, _ := decimal.NewFromString(pm)
		dv, _ := decimal.NewFromString(dp)
		dueIDs = append(dueIDs, id)
		sumInterest = sumInterest.Add(iv)
		sumPrincipal = sumPrincipal.Add(pv)
		sumPayment = sumPayment.Add(mv)
		sumDepr = sumDepr.Add(dv)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, decimal.Zero, 0, err
	}
	if len(dueIDs) == 0 {
		return nil, decimal.Zero, 0, nil
	}

	ids := map[string]uuid.UUID{}
	for _, code := range []string{leaseInterestCode, rouDeprCode, leaseLiabCode, defaultFundingCode, rouAccumDepCode} {
		id, err := r.accountIDByCode(ctx, code)
		if err != nil {
			return nil, decimal.Zero, 0, err
		}
		ids[code] = id
	}

	var lines []ResolvedLine
	order := 0
	add := func(code string, debit, credit decimal.Decimal, memo string) {
		if debit.IsZero() && credit.IsZero() {
			return
		}
		lines = append(lines, ResolvedLine{AccountID: ids[code], Debit: debit, Credit: credit, Memo: memo, LineOrder: order})
		order++
	}
	add(leaseInterestCode, sumInterest, decimal.Zero, "Lease interest "+period)
	add(rouDeprCode, sumDepr, decimal.Zero, "ROU depreciation "+period)
	add(leaseLiabCode, sumPrincipal, decimal.Zero, "Lease principal "+period)
	add(defaultFundingCode, decimal.Zero, sumPayment, "Lease payment "+period)
	add(rouAccumDepCode, decimal.Zero, sumDepr, "Accum ROU depreciation "+period)

	eventID := "leaserun:" + period
	src := "iag.finance"
	entry, err := r.BookPostedEntry(ctx, CreateJournalParams{
		Description:    "Lease period " + period,
		SourceEventID:  &eventID,
		SourceService:  &src,
		AccountingDate: postedAt,
		Lines:          lines,
	}, eventID, "finance.lease.run", postedAt, func(ctx context.Context, tx pgx.Tx, entryID uuid.UUID) error {
		for _, id := range dueIDs {
			if _, err := tx.Exec(ctx, `
				UPDATE lease_schedule_lines SET journal_entry_id = $2, recognized_at = NOW()
				WHERE id = $1 AND journal_entry_id IS NULL
			`, id, entryID); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `
			UPDATE leases l SET status = 'completed'
			WHERE l.status = 'active'
			  AND NOT EXISTS (
				SELECT 1 FROM lease_schedule_lines s
				WHERE s.lease_id = l.id AND s.journal_entry_id IS NULL
			  )
		`)
		return err
	}, audit)
	if err != nil {
		return nil, decimal.Zero, 0, err
	}
	return entry, sumInterest.Add(sumPrincipal), len(dueIDs), nil
}

// GetLeaseByRef returns the lease for a reference.
func (r *Repository) GetLeaseByRef(ctx context.Context, leaseRef string) (*Lease, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, lease_ref, description, currency, monthly_payment::text, annual_rate::text, term_months, start_period, rou_asset::text, initial_liability::text, status, created_at
		FROM leases WHERE lease_ref = $1
	`, leaseRef)
	return scanLease(row)
}

// ListLeases returns recent leases, newest first.
func (r *Repository) ListLeases(ctx context.Context, limit int) ([]Lease, error) {
	if limit <= 0 || limit > 200 {
		limit = 60
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, lease_ref, description, currency, monthly_payment::text, annual_rate::text, term_months, start_period, rou_asset::text, initial_liability::text, status, created_at
		FROM leases ORDER BY created_at DESC LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Lease
	for rows.Next() {
		l, err := scanLease(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *l)
	}
	return out, rows.Err()
}

func scanLease(row scannable) (*Lease, error) {
	var l Lease
	var pay, rate, rou, liab string
	if err := row.Scan(&l.ID, &l.LeaseRef, &l.Description, &l.Currency, &pay, &rate, &l.TermMonths, &l.StartPeriod, &rou, &liab, &l.Status, &l.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrLeaseNotFound
		}
		return nil, err
	}
	l.MonthlyPayment, _ = decimal.NewFromString(pay)
	l.AnnualRate, _ = decimal.NewFromString(rate)
	l.ROUAsset, _ = decimal.NewFromString(rou)
	l.InitialLiability, _ = decimal.NewFromString(liab)
	return &l, nil
}
