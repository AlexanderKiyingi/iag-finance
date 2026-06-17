package ledger

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/repository"
)

func (s *Service) ListApprovalTiers(ctx context.Context) ([]repository.ApprovalTier, error) {
	return s.repo.ListApprovalTiers(ctx)
}

func (s *Service) ListApprovals(ctx context.Context, status string, limit, offset int) ([]repository.Approval, error) {
	return s.repo.ListApprovals(ctx, status, limit, offset)
}

func (s *Service) GetApproval(ctx context.Context, id uuid.UUID) (*repository.Approval, error) {
	return s.repo.GetApproval(ctx, id)
}

// ApprovalRequired reports whether an amount reaches the first approval band
// (used by the enforcement guards on direct post/payment endpoints).
func (s *Service) ApprovalRequired(ctx context.Context, amount decimal.Decimal) (bool, error) {
	tiers, err := s.repo.ListApprovalTiers(ctx)
	if err != nil {
		return false, err
	}
	return len(repository.RequiredApprovalTiers(tiers, amount)) > 0, nil
}

// SubmitForApproval creates a pending approval for a high-value action. Returns
// (nil, false, nil) when the amount is below the first band — no approval is
// needed and the caller may proceed directly.
func (s *Service) SubmitForApproval(ctx context.Context, targetType string, amount decimal.Decimal, currency string, payload map[string]any, requestedBy, description string) (*repository.Approval, bool, error) {
	required, err := s.ApprovalRequired(ctx, amount)
	if err != nil {
		return nil, false, err
	}
	if !required {
		return nil, false, nil
	}
	a, err := s.repo.CreateApproval(ctx, targetType, amount, currency, payload, requestedBy, description)
	return a, true, err
}

// ApproveApproval records the caller's tier signature and, once every required
// tier has signed, executes the underlying action (posts the journal or applies
// the payment) and marks the approval executed.
func (s *Service) ApproveApproval(ctx context.Context, id uuid.UUID, actor string, hasPerm func(string) bool, note string) (*repository.Approval, *repository.ApprovalProgress, error) {
	finalize, prog, err := s.repo.RecordApprovalTier(ctx, id, actor, hasPerm, note)
	if err != nil {
		return nil, nil, err
	}
	if finalize {
		if err := s.executeApproval(ctx, id, actor); err != nil {
			// The approval stays 'approved' (retryable); surface the error.
			return nil, nil, err
		}
	}
	a, err := s.repo.GetApproval(ctx, id)
	return a, prog, err
}

func (s *Service) RejectApproval(ctx context.Context, id uuid.UUID, actor string, hasPerm func(string) bool, note string) (*repository.Approval, *repository.ApprovalProgress, error) {
	prog, err := s.repo.RejectApproval(ctx, id, actor, hasPerm, note)
	if err != nil {
		return nil, nil, err
	}
	a, err := s.repo.GetApproval(ctx, id)
	return a, prog, err
}

// executeApproval runs the approved action and records the result. The journal
// post / payment apply each open their own transaction, so this runs after the
// tier sign-off commits; a failure leaves the approval 'approved' for retry.
func (s *Service) executeApproval(ctx context.Context, id uuid.UUID, actor string) error {
	a, err := s.repo.GetApproval(ctx, id)
	if err != nil {
		return err
	}
	if a.Status == "executed" {
		return nil
	}
	switch a.TargetType {
	case "journal":
		entryID, err := uuid.Parse(approvalString(a.Payload, "entryId"))
		if err != nil {
			return fmt.Errorf("approval %s: invalid entryId: %w", id, err)
		}
		entry, err := s.PostJournalEntry(ctx, entryID, actor)
		if err != nil {
			return err
		}
		result := ""
		if entry != nil {
			result = entry.EntryNumber
		}
		return s.repo.MarkApprovalExecuted(ctx, id, result)
	case "payment":
		itemID, err := uuid.Parse(approvalString(a.Payload, "openItemId"))
		if err != nil {
			return fmt.Errorf("approval %s: invalid openItemId: %w", id, err)
		}
		paymentRef := approvalString(a.Payload, "paymentRef")
		if approvalString(a.Payload, "direction") == "ar" {
			if _, _, err := s.ApplyARPayment(ctx, itemID, a.Amount, a.Currency, paymentRef, actor, nil); err != nil {
				return err
			}
		} else {
			if _, _, err := s.ApplyAPPayment(ctx, itemID, a.Amount, a.Currency, paymentRef, actor, nil); err != nil {
				return err
			}
		}
		return s.repo.MarkApprovalExecuted(ctx, id, paymentRef)
	default:
		return fmt.Errorf("approval %s: unknown target type %q", id, a.TargetType)
	}
}

func approvalString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}
