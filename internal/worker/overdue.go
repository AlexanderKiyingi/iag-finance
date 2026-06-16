package worker

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/iag-finance/backend/internal/config"
	"github.com/iag-finance/backend/internal/events"
	"github.com/iag-finance/backend/internal/ledger"
)

const overdueTemplate = "scm.payment-overdue"

// OverdueNotifier scans overdue AR and publishes scm-payment-overdue emails.
type OverdueNotifier struct {
	cfg    config.Config
	ledger *ledger.Service
	events *events.Bus
}

func NewOverdueNotifier(cfg config.Config, ledgerSvc *ledger.Service, bus *events.Bus) *OverdueNotifier {
	return &OverdueNotifier{cfg: cfg, ledger: ledgerSvc, events: bus}
}

func (w *OverdueNotifier) Run(ctx context.Context) {
	if !w.cfg.OverdueCronEnabled || w.cfg.OverdueNotifyEmail == "" {
		slog.Info("overdue cron disabled or OVERDUE_NOTIFY_EMAIL unset")
		return
	}
	// Unset → daily default; otherwise honour the configured cadence but never
	// faster than a 5m floor (previously any sub-5m value was forced to 24h,
	// surprising operators who set a short interval for testing).
	interval := w.cfg.OverdueCronInterval
	switch {
	case interval <= 0:
		interval = 24 * time.Hour
	case interval < 5*time.Minute:
		interval = 5 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	w.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

func (w *OverdueNotifier) tick(ctx context.Context) {
	items, err := w.ledger.ListOverdueAR(ctx, 24)
	if err != nil {
		slog.Warn("overdue scan failed", "err", err)
		return
	}
	if len(items) == 0 {
		return
	}
	if w.events == nil || !w.events.NotificationsEnabled() {
		slog.Warn("overdue items found but notification publisher disabled", "count", len(items))
		return
	}
	href := w.cfg.OverdueNotifyHref
	if href == "" {
		href = "/api/v1/finance/v1/ar/items"
	}
	w.events.PublishNotification(ctx, w.cfg.OverdueNotifyEmail, overdueTemplate, map[string]string{
		"PaymentCount": strconv.Itoa(len(items)),
		"Href":         href,
	})
	refs := make([]string, 0, len(items))
	for _, it := range items {
		refs = append(refs, it.DocumentRef)
	}
	if err := w.ledger.MarkOverdueNotified(ctx, refs); err != nil {
		slog.Warn("mark overdue notified failed", "err", err)
	}
	slog.Info("overdue notification sent", "count", len(items), "recipient", w.cfg.OverdueNotifyEmail)
}

func (w *OverdueNotifier) String() string {
	return fmt.Sprintf("overdue-notifier interval=%s", w.cfg.OverdueCronInterval)
}
