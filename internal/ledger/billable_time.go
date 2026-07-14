package ledger

import (
	"context"

	"github.com/iag-finance/backend/internal/repository"
)

// RecordTimeEntry persists an unbilled billable-time entry. No GL is posted at
// capture — revenue is recognised when the time is invoiced.
func (s *Service) RecordTimeEntry(ctx context.Context, in repository.CreateTimeEntryInput) (*repository.TimeEntry, error) {
	return s.repo.CreateTimeEntry(ctx, in)
}

func (s *Service) ListTimeEntries(ctx context.Context, limit, offset int) ([]repository.TimeEntry, error) {
	return s.repo.ListTimeEntries(ctx, limit, offset)
}
