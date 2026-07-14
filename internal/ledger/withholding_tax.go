package ledger

import (
	"context"

	"github.com/iag-finance/backend/internal/repository"
)

// RecordWHTReceipt records a withholding-tax certificate and posts the recoverable
// asset (Dr 1150 WHT Recoverable / Cr 1100 AR) as of the receipt date, refusing a
// closed period.
func (s *Service) RecordWHTReceipt(ctx context.Context, in repository.CreateWHTReceiptInput) (*repository.WHTReceipt, error) {
	closed, err := s.repo.IsPeriodClosed(ctx, in.ReceiptDate.Format("2006-01"))
	if err != nil {
		return nil, err
	}
	if closed {
		return nil, ErrPeriodClosed
	}
	return s.repo.CreateWHTReceipt(ctx, in)
}

func (s *Service) ListWHTReceipts(ctx context.Context, limit, offset int) ([]repository.WHTReceipt, error) {
	return s.repo.ListWHTReceipts(ctx, limit, offset)
}
