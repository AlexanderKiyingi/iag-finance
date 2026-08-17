package worker

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/iag-finance/backend/internal/events"
	"github.com/iag-finance/backend/internal/repository"
)

const outboxBatchSize = 100

// OutboxRelay delivers events recorded in the transactional outbox. It polls for
// unpublished rows, publishes each via the bus (returning the error so failures
// are retried, not lost), and marks delivered ones. Consumer-side idempotency
// absorbs the at-least-once duplicates this can produce.
type OutboxRelay struct {
	repo     *repository.Repository
	bus      *events.Bus
	interval time.Duration
}

func NewOutboxRelay(repo *repository.Repository, bus *events.Bus, interval time.Duration) *OutboxRelay {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &OutboxRelay{repo: repo, bus: bus, interval: interval}
}

func (w *OutboxRelay) Run(ctx context.Context) {
	if w.bus == nil || !w.bus.Enabled() {
		slog.Info("outbox relay disabled (event publishing off)")
		return
	}
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		w.drainLocked(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// drainLocked runs a drain pass on whichever replica holds the relay lock.
//
// The fetch selects unpublished rows without claiming them, so two replicas
// draining at once publish the same events twice. Consumer-side dedupe absorbs
// that, which is why this is wasted work rather than corruption — but it also
// doubles broker traffic and retry churn for no gain, and "the consumer will
// sort it out" is a thin guarantee to lean on for every event the platform
// emits. One drainer at a time is cheaper and easier to reason about.
func (w *OutboxRelay) drainLocked(ctx context.Context) {
	ran, err := w.repo.WithJobLock(ctx, repository.JobLockOutboxRelay, func(ctx context.Context) error {
		w.drain(ctx)
		return nil
	})
	if err != nil {
		slog.Error("outbox relay lock failed", "err", err)
		return
	}
	if !ran {
		slog.Debug("outbox relay skipped; another instance holds the lock")
	}
}

func (w *OutboxRelay) drain(ctx context.Context) {
	rows, err := w.repo.FetchUnpublishedOutbox(ctx, outboxBatchSize)
	if err != nil {
		slog.Error("outbox fetch failed", "err", err)
		return
	}
	for _, row := range rows {
		var payload map[string]any
		if len(row.Payload) > 0 {
			if err := json.Unmarshal(row.Payload, &payload); err != nil {
				// Poison payload: record the failure and skip so it doesn't block
				// the queue. It stays unpublished for inspection.
				slog.Error("outbox payload decode failed", "id", row.ID, "err", err)
				_ = w.repo.MarkOutboxFailed(ctx, row.ID, "payload decode: "+err.Error())
				continue
			}
		}
		if err := w.bus.PublishRaw(ctx, row.Topic, row.PartitionKey, row.EventID, row.EventType, payload); err != nil {
			slog.Warn("outbox publish failed; will retry", "id", row.ID, "attempts", row.Attempts, "err", err)
			_ = w.repo.MarkOutboxFailed(ctx, row.ID, err.Error())
			continue
		}
		if err := w.repo.MarkOutboxPublished(ctx, row.ID); err != nil {
			slog.Error("outbox mark published failed", "id", row.ID, "err", err)
		}
	}
}
