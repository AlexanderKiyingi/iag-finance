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

// Three-way match detection over the GR/IR accruals. This never changes the
// clearing GL (which nets in either order); it classifies each PO's accrued-vs-
// cleared state and raises review exceptions, and can write a confirmed residual
// to Purchase Price Variance (5150).

const (
	ppvCode          = "5150"
	grIRClearingCode = "2150"
)

// ErrNoVariance indicates a PO's GR/IR residual is already within tolerance.
var ErrNoVariance = errors.New("no GR/IR variance to write off")

// ErrMatchExceptionNotFound indicates the exception id does not exist.
var ErrMatchExceptionNotFound = errors.New("match exception not found")

// MatchException is one entry in the three-way-match review queue.
type MatchException struct {
	ID          uuid.UUID `json:"id"`
	PORef       string    `json:"poRef"`
	DocumentRef string    `json:"documentRef"`
	Type        string    `json:"type"`
	Detail      string    `json:"detail"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
}

// MatchTolerance returns the allowed net-difference fraction (scope 'default').
func (r *Repository) MatchTolerance(ctx context.Context) decimal.Decimal {
	var s string
	if err := r.pool.QueryRow(ctx, `SELECT pct::text FROM match_tolerance WHERE scope = 'default'`).Scan(&s); err != nil {
		return decimal.NewFromFloat(0.02)
	}
	d, _ := decimal.NewFromString(s)
	return d
}

// DetectMatchExceptions classifies every PO accrual and (idempotently, on
// (po_ref,type)) raises exceptions: orphan_invoice when an invoice cleared with
// no goods receipt, price_variance when both sides posted but the net residual
// exceeds tolerance. It also stamps grni_accruals.match_status. Returns the count
// of open exceptions after the pass.
func (r *Repository) DetectMatchExceptions(ctx context.Context) (int, error) {
	tolerance := r.MatchTolerance(ctx)
	rows, err := r.pool.Query(ctx, `SELECT po_ref, accrued::text, cleared::text FROM grni_accruals`)
	if err != nil {
		return 0, err
	}
	type acc struct {
		po               string
		accrued, cleared decimal.Decimal
	}
	var accs []acc
	for rows.Next() {
		var a acc
		var ac, cl string
		if err := rows.Scan(&a.po, &ac, &cl); err != nil {
			rows.Close()
			return 0, err
		}
		a.accrued, _ = decimal.NewFromString(ac)
		a.cleared, _ = decimal.NewFromString(cl)
		accs = append(accs, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	for _, a := range accs {
		open := a.accrued.Sub(a.cleared)
		status := "open"
		switch {
		case a.cleared.IsPositive() && a.accrued.IsZero():
			status = "pending" // invoice arrived before goods receipt
			if err := r.upsertMatchException(ctx, a.po, "orphan_invoice",
				"invoice cleared with no goods-receipt accrual"); err != nil {
				return 0, err
			}
		case a.accrued.IsPositive() && a.cleared.IsPositive():
			limit := a.accrued.Mul(tolerance).Abs()
			if open.Abs().GreaterThan(limit) {
				status = "variance"
				typ := "price_variance"
				if open.IsNegative() {
					typ = "over_invoice"
				}
				if err := r.upsertMatchException(ctx, a.po, typ,
					fmt.Sprintf("accrued %s vs cleared %s (residual %s, tolerance %s)",
						a.accrued.String(), a.cleared.String(), open.String(), limit.String())); err != nil {
					return 0, err
				}
			} else {
				status = "matched"
				// Within tolerance — auto-resolve any prior variance exceptions.
				if _, err := r.pool.Exec(ctx, `
					UPDATE match_exceptions SET status='resolved', resolved_at=NOW(), resolved_by='system:match'
					WHERE po_ref=$1 AND status='open' AND type IN ('price_variance','over_invoice','orphan_invoice')
				`, a.po); err != nil {
					return 0, err
				}
			}
		}
		if _, err := r.pool.Exec(ctx, `UPDATE grni_accruals SET match_status=$2, updated_at=NOW() WHERE po_ref=$1`, a.po, status); err != nil {
			return 0, err
		}
	}

	var open int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM match_exceptions WHERE status='open'`).Scan(&open); err != nil {
		return 0, err
	}
	return open, nil
}

func (r *Repository) upsertMatchException(ctx context.Context, poRef, typ, detail string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO match_exceptions (po_ref, type, detail)
		VALUES ($1, $2, $3)
		ON CONFLICT (po_ref, type) DO UPDATE
		SET detail = EXCLUDED.detail
		WHERE match_exceptions.status = 'open'
	`, poRef, typ, detail)
	return err
}

// ListMatchExceptions returns exceptions, optionally filtered by status.
func (r *Repository) ListMatchExceptions(ctx context.Context, status string, limit int) ([]MatchException, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT id, po_ref, document_ref, type, detail, status, created_at FROM match_exceptions`
	args := []any{}
	if status != "" {
		q += ` WHERE status = $1`
		args = append(args, status)
	}
	q += ` ORDER BY created_at DESC LIMIT ` + itoa(len(args)+1)
	args = append(args, limit)
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MatchException
	for rows.Next() {
		var e MatchException
		if err := rows.Scan(&e.ID, &e.PORef, &e.DocumentRef, &e.Type, &e.Detail, &e.Status, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ResolveMatchException marks an exception resolved.
func (r *Repository) ResolveMatchException(ctx context.Context, id uuid.UUID, actor string) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE match_exceptions SET status='resolved', resolved_at=NOW(), resolved_by=$2
		WHERE id=$1 AND status='open'
	`, id, actor)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrMatchExceptionNotFound
	}
	return nil
}

// WriteOffGRNIVariance moves a PO's residual GR/IR balance to Purchase Price
// Variance and marks the accrual matched, so a confirmed price difference lands
// in P&L instead of lingering in the clearing account. Residual = accrued −
// cleared (credit-normal on 2150). Idempotent on grir.variance:<poRef>:<date>.
func (r *Repository) WriteOffGRNIVariance(ctx context.Context, poRef string, postedAt time.Time, audit *AuditInfo) (*domain.JournalEntry, error) {
	var accS, clS, currency string
	err := r.pool.QueryRow(ctx, `SELECT accrued::text, cleared::text, currency FROM grni_accruals WHERE po_ref=$1`, poRef).Scan(&accS, &clS, &currency)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoVariance
	}
	if err != nil {
		return nil, err
	}
	accrued, _ := decimal.NewFromString(accS)
	cleared, _ := decimal.NewFromString(clS)
	residual := accrued.Sub(cleared) // credit-normal residual on GR/IR
	if residual.IsZero() {
		return nil, ErrNoVariance
	}
	if currency == "" {
		currency = r.baseCurrency
	}
	clearingID, err := r.accountIDByCode(ctx, grIRClearingCode)
	if err != nil {
		return nil, err
	}
	ppvID, err := r.accountIDByCode(ctx, ppvCode)
	if err != nil {
		return nil, err
	}
	var lines []ResolvedLine
	if residual.IsPositive() {
		// GR/IR holds a leftover credit (goods > invoiced): clear it to a PPV credit (gain).
		lines = []ResolvedLine{
			{AccountID: clearingID, Debit: residual, Memo: "Clear GR/IR variance " + poRef, LineOrder: 0},
			{AccountID: ppvID, Credit: residual, Memo: "Purchase price variance", LineOrder: 1},
		}
	} else {
		amt := residual.Neg()
		// GR/IR holds a leftover debit (invoiced > goods): expense the variance.
		lines = []ResolvedLine{
			{AccountID: ppvID, Debit: amt, Memo: "Purchase price variance", LineOrder: 0},
			{AccountID: clearingID, Credit: amt, Memo: "Clear GR/IR variance " + poRef, LineOrder: 1},
		}
	}
	eventID := "grir.variance:" + poRef + ":" + postedAt.Format("2006-01-02")
	src := "iag.finance"
	entry, err := r.BookPostedEntry(ctx, CreateJournalParams{
		Description:    "GR/IR variance write-off — PO " + poRef,
		SourceEventID:  &eventID,
		SourceService:  &src,
		Currency:       currency,
		AccountingDate: postedAt,
		Lines:          lines,
	}, eventID, "finance.grir.variance", postedAt, func(ctx context.Context, tx pgx.Tx, entryID uuid.UUID) error {
		// Equalise accrued/cleared so the PO is fully matched.
		if _, err := tx.Exec(ctx, `UPDATE grni_accruals SET cleared = accrued, match_status='matched', updated_at=NOW() WHERE po_ref=$1`, poRef); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			UPDATE match_exceptions SET status='resolved', resolved_at=NOW(), resolved_by='system:ppv'
			WHERE po_ref=$1 AND status='open'
		`, poRef)
		return err
	}, audit)
	if err != nil {
		return nil, err
	}
	return entry, nil
}
