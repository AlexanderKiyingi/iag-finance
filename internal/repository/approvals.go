package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

var (
	ErrApprovalNotFound  = errors.New("approval not found")
	ErrApprovalForbidden = errors.New("approval not permitted")
	ErrApprovalConflict  = errors.New("approval is not pending")
)

type ApprovalTier struct {
	Tier         int              `json:"tier"`
	Label        string           `json:"label"`
	MinAmount    decimal.Decimal  `json:"minAmount"`
	MaxAmount    *decimal.Decimal `json:"maxAmount,omitempty"`
	RequiredPerm string           `json:"requiredPerm"`
}

type Approval struct {
	ID          uuid.UUID      `json:"id"`
	TargetType  string         `json:"targetType"`
	Amount      decimal.Decimal `json:"amount"`
	Currency    string         `json:"currency"`
	Payload     map[string]any `json:"payload"`
	Status      string         `json:"status"`
	RequestedBy string         `json:"requestedBy"`
	Description string         `json:"description"`
	ResultRef   string         `json:"resultRef"`
	CreatedAt   time.Time      `json:"createdAt"`
	DecidedAt   *time.Time     `json:"decidedAt,omitempty"`
}

type ApprovalProgress struct {
	RequiredTiers []int  `json:"requiredTiers"`
	ApprovedTiers []int  `json:"approvedTiers"`
	NextTier      *int   `json:"nextTier,omitempty"`
	NextPerm      string `json:"nextPerm,omitempty"`
	Complete      bool   `json:"complete"`
}

const approvalCols = `id, target_type, amount::text, currency, payload::text, status, requested_by, description, result_ref, created_at, decided_at`

func scanApproval(row pgx.Row) (*Approval, error) {
	var a Approval
	var amountS, payloadS string
	if err := row.Scan(&a.ID, &a.TargetType, &amountS, &a.Currency, &payloadS, &a.Status, &a.RequestedBy, &a.Description, &a.ResultRef, &a.CreatedAt, &a.DecidedAt); err != nil {
		return nil, err
	}
	a.Amount, _ = decimal.NewFromString(amountS)
	if payloadS != "" {
		_ = json.Unmarshal([]byte(payloadS), &a.Payload)
	}
	return &a, nil
}

func (r *Repository) ListApprovalTiers(ctx context.Context) ([]ApprovalTier, error) {
	return scanApprovalTiers(r.pool.Query(ctx, `
		SELECT tier, label, min_amount::text, max_amount::text, required_perm FROM finance_approval_tiers ORDER BY tier`))
}

func (r *Repository) listApprovalTiersTx(ctx context.Context, tx pgx.Tx) ([]ApprovalTier, error) {
	return scanApprovalTiers(tx.Query(ctx, `
		SELECT tier, label, min_amount::text, max_amount::text, required_perm FROM finance_approval_tiers ORDER BY tier`))
}

func scanApprovalTiers(rows pgx.Rows, err error) ([]ApprovalTier, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ApprovalTier
	for rows.Next() {
		var t ApprovalTier
		var minS string
		var maxS *string
		if err := rows.Scan(&t.Tier, &t.Label, &minS, &maxS, &t.RequiredPerm); err != nil {
			return nil, err
		}
		t.MinAmount, _ = decimal.NewFromString(minS)
		if maxS != nil {
			if m, err := decimal.NewFromString(*maxS); err == nil {
				t.MaxAmount = &m
			}
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// RequiredApprovalTiers is the subset of bands the amount exceeds — each must
// sign off. Empty means the amount is below the first band (no approval needed).
func RequiredApprovalTiers(tiers []ApprovalTier, amount decimal.Decimal) []ApprovalTier {
	var out []ApprovalTier
	for _, t := range tiers {
		if amount.GreaterThan(t.MinAmount) {
			out = append(out, t)
		}
	}
	return out
}

func (r *Repository) CreateApproval(ctx context.Context, targetType string, amount decimal.Decimal, currency string, payload map[string]any, requestedBy, description string) (*Approval, error) {
	if currency == "" {
		currency = r.baseCurrency
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return scanApproval(r.pool.QueryRow(ctx, `
		INSERT INTO finance_approvals (target_type, amount, currency, payload, requested_by, description)
		VALUES ($1, $2, $3, $4::jsonb, $5, $6)
		RETURNING `+approvalCols,
		targetType, amount, currency, string(payloadJSON), requestedBy, description))
}

func (r *Repository) GetApproval(ctx context.Context, id uuid.UUID) (*Approval, error) {
	a, err := scanApproval(r.pool.QueryRow(ctx, `SELECT `+approvalCols+` FROM finance_approvals WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrApprovalNotFound
	}
	return a, err
}

func (r *Repository) ListApprovals(ctx context.Context, status string, limit, offset int) ([]Approval, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	q := `SELECT ` + approvalCols + ` FROM finance_approvals`
	args := []any{}
	if status != "" {
		q += ` WHERE status = $1`
		args = append(args, status)
	}
	q += fmt.Sprintf(` ORDER BY created_at DESC LIMIT %d OFFSET %d`, limit, offset)
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Approval
	for rows.Next() {
		a, err := scanApproval(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (r *Repository) MarkApprovalExecuted(ctx context.Context, id uuid.UUID, resultRef string) error {
	_, err := r.pool.Exec(ctx, `UPDATE finance_approvals SET status = 'executed', result_ref = $2 WHERE id = $1`, id, resultRef)
	return err
}

// RecordApprovalTier signs the lowest not-yet-cleared required tier under a row
// lock, enforcing distinct approvers, requester-≠-approver, and the tier's
// permission (hasPerm). It returns finalize=true and flips the approval to
// 'approved' once every required tier has signed; the caller then executes the
// target. Idempotent at the tier level via the partial unique index.
func (r *Repository) RecordApprovalTier(ctx context.Context, id uuid.UUID, actor string, hasPerm func(string) bool, note string) (bool, *ApprovalProgress, error) {
	if hasPerm == nil {
		hasPerm = func(string) bool { return false }
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, nil, err
	}
	defer tx.Rollback(ctx)

	var status, amountS, requestedBy string
	err = tx.QueryRow(ctx, `SELECT status, amount::text, requested_by FROM finance_approvals WHERE id = $1 FOR UPDATE`, id).Scan(&status, &amountS, &requestedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil, ErrApprovalNotFound
	}
	if err != nil {
		return false, nil, err
	}
	if status != "pending" {
		return false, nil, fmt.Errorf("%w: %s", ErrApprovalConflict, status)
	}
	if sameApprovalActor(actor, requestedBy) {
		return false, nil, fmt.Errorf("%w: the requester cannot approve their own request", ErrApprovalForbidden)
	}
	amount, _ := decimal.NewFromString(amountS)

	tiers, err := r.listApprovalTiersTx(ctx, tx)
	if err != nil {
		return false, nil, err
	}
	required := RequiredApprovalTiers(tiers, amount)
	approved, err := r.approvedApprovalTiersTx(ctx, tx, id)
	if err != nil {
		return false, nil, err
	}
	if dup, err := r.actorApprovedTx(ctx, tx, id, actor); err != nil {
		return false, nil, err
	} else if dup {
		return false, nil, fmt.Errorf("%w: %s already approved a tier", ErrApprovalForbidden, actor)
	}

	var next *ApprovalTier
	for i := range required {
		if !containsApprovalTier(approved, required[i].Tier) {
			next = &required[i]
			break
		}
	}

	finalize := false
	if next == nil {
		finalize = true
	} else {
		if !hasPerm(next.RequiredPerm) {
			return false, nil, fmt.Errorf("%w: approving tier %d requires %s", ErrApprovalForbidden, next.Tier, next.RequiredPerm)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO finance_approval_decisions (approval_id, tier, actor, decision, note)
			VALUES ($1, $2, $3, 'approved', $4)`, id, next.Tier, actor, note); err != nil {
			return false, nil, err
		}
		approved = append(approved, next.Tier)
		finalize = allApprovalTiers(required, approved)
	}

	if finalize {
		if _, err := tx.Exec(ctx, `UPDATE finance_approvals SET status = 'approved', decided_at = NOW() WHERE id = $1`, id); err != nil {
			return false, nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return false, nil, err
	}
	return finalize, buildApprovalProgress(required, approved, finalize), nil
}

// RejectApproval rejects a pending approval. Any holder of a required tier
// permission (or the requester withdrawing) may reject.
func (r *Repository) RejectApproval(ctx context.Context, id uuid.UUID, actor string, hasPerm func(string) bool, note string) (*ApprovalProgress, error) {
	if hasPerm == nil {
		hasPerm = func(string) bool { return false }
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var status, amountS, requestedBy string
	err = tx.QueryRow(ctx, `SELECT status, amount::text, requested_by FROM finance_approvals WHERE id = $1 FOR UPDATE`, id).Scan(&status, &amountS, &requestedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrApprovalNotFound
	}
	if err != nil {
		return nil, err
	}
	if status != "pending" {
		return nil, fmt.Errorf("%w: %s", ErrApprovalConflict, status)
	}
	amount, _ := decimal.NewFromString(amountS)
	tiers, err := r.listApprovalTiersTx(ctx, tx)
	if err != nil {
		return nil, err
	}
	required := RequiredApprovalTiers(tiers, amount)

	allowed := sameApprovalActor(actor, requestedBy) || len(required) == 0
	rejectTier := 0
	for _, t := range required {
		if hasPerm(t.RequiredPerm) {
			allowed = true
			if rejectTier == 0 {
				rejectTier = t.Tier
			}
		}
	}
	if !allowed {
		return nil, fmt.Errorf("%w: rejecting requires a tier approval permission", ErrApprovalForbidden)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO finance_approval_decisions (approval_id, tier, actor, decision, note)
		VALUES ($1, $2, $3, 'rejected', $4)`, id, rejectTier, actor, note); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE finance_approvals SET status = 'rejected', decided_at = NOW() WHERE id = $1`, id); err != nil {
		return nil, err
	}
	approved, _ := r.approvedApprovalTiersTx(ctx, tx, id)
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return buildApprovalProgress(required, approved, false), nil
}

func (r *Repository) approvedApprovalTiersTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) ([]int, error) {
	rows, err := tx.Query(ctx, `SELECT tier FROM finance_approval_decisions WHERE approval_id = $1 AND decision = 'approved' ORDER BY tier`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var t int
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *Repository) actorApprovedTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, actor string) (bool, error) {
	if strings.TrimSpace(actor) == "" {
		return false, nil
	}
	var exists bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM finance_approval_decisions
		WHERE approval_id = $1 AND decision = 'approved' AND lower(actor) = lower($2))`, id, actor).Scan(&exists)
	return exists, err
}

func containsApprovalTier(xs []int, v int) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func allApprovalTiers(required []ApprovalTier, approved []int) bool {
	for _, t := range required {
		if !containsApprovalTier(approved, t.Tier) {
			return false
		}
	}
	return true
}

func buildApprovalProgress(required []ApprovalTier, approved []int, complete bool) *ApprovalProgress {
	p := &ApprovalProgress{ApprovedTiers: approved, Complete: complete}
	for _, t := range required {
		p.RequiredTiers = append(p.RequiredTiers, t.Tier)
		if p.NextTier == nil && !containsApprovalTier(approved, t.Tier) {
			tier := t.Tier
			p.NextTier = &tier
			p.NextPerm = t.RequiredPerm
		}
	}
	if complete {
		p.NextTier = nil
		p.NextPerm = ""
	}
	return p
}

func sameApprovalActor(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" || strings.EqualFold(a, "unknown") {
		return false
	}
	return strings.EqualFold(a, b)
}
