package ledger

import (
	"context"

	"github.com/iag-finance/backend/internal/repository"
)

// RegisterIntangibleAsset capitalises an intangible (IAS 38) into the finance
// subledger. When in.CapitalizeFromAccount is set it posts the reclass
// Dr 1700 Intangible Assets / Cr <source> for the cost (as of the in-service
// date), refusing if that period is closed; empty leaves it record-only.
func (s *Service) RegisterIntangibleAsset(ctx context.Context, in repository.CreateIntangibleAssetInput) (*repository.IntangibleAsset, error) {
	if in.CapitalizeFromAccount != "" {
		closed, err := s.repo.IsPeriodClosed(ctx, in.InServiceDate.Format("2006-01"))
		if err != nil {
			return nil, err
		}
		if closed {
			return nil, ErrPeriodClosed
		}
	}
	return s.repo.CreateIntangibleAsset(ctx, in)
}

func (s *Service) ListIntangibleAssets(ctx context.Context, limit, offset int) ([]repository.IntangibleAsset, error) {
	return s.repo.ListIntangibleAssets(ctx, limit, offset)
}
