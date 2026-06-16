package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/iag-finance/backend/internal/domain"
)

type AuditLogParams struct {
	EventType     string
	ActorID       *uuid.UUID
	ActorEmail    string
	ResourceType  *string
	ResourceID    *string
	HTTPMethod    *string
	HTTPPath      *string
	StatusCode    *int
	IPAddress     *string
	UserAgent     *string
	CorrelationID *string
	Metadata      map[string]any
}

type AuditListFilter struct {
	EventType string
	ActorID   *uuid.UUID
	Resource  string
	From      *time.Time
	To        *time.Time
	Limit     int
	Offset    int
}

func (r *Repository) InsertAuditLog(ctx context.Context, p AuditLogParams) error {
	meta := []byte("{}")
	if p.Metadata != nil {
		b, err := json.Marshal(p.Metadata)
		if err != nil {
			return err
		}
		meta = b
	}

	_, err := r.pool.Exec(ctx, `
		INSERT INTO finance_audit_log (
			event_type, actor_id, actor_email, resource_type, resource_id,
			http_method, http_path, status_code, ip_address, user_agent, correlation_id, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`, p.EventType, p.ActorID, p.ActorEmail, p.ResourceType, p.ResourceID,
		p.HTTPMethod, p.HTTPPath, p.StatusCode, p.IPAddress, p.UserAgent, p.CorrelationID, meta)
	return err
}

func (r *Repository) ListAuditLogs(ctx context.Context, f AuditListFilter) ([]domain.AuditEntry, int, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}

	where := "WHERE 1=1"
	args := []any{}
	argN := 1

	if f.EventType != "" {
		where += fmt.Sprintf(" AND event_type = $%d", argN)
		args = append(args, f.EventType)
		argN++
	}
	if f.ActorID != nil {
		where += fmt.Sprintf(" AND actor_id = $%d", argN)
		args = append(args, *f.ActorID)
		argN++
	}
	if f.Resource != "" {
		where += fmt.Sprintf(" AND (resource_type = $%d OR resource_id = $%d)", argN, argN+1)
		args = append(args, f.Resource, f.Resource)
		argN += 2
	}
	if f.From != nil {
		where += fmt.Sprintf(" AND created_at >= $%d", argN)
		args = append(args, *f.From)
		argN++
	}
	if f.To != nil {
		where += fmt.Sprintf(" AND created_at <= $%d", argN)
		args = append(args, *f.To)
		argN++
	}

	countSQL := "SELECT COUNT(*) FROM finance_audit_log " + where
	var total int
	if err := r.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	listSQL := `
		SELECT id, event_type, actor_id, actor_email, resource_type, resource_id,
			http_method, http_path, status_code, ip_address, user_agent, correlation_id, metadata, created_at
		FROM finance_audit_log ` + where + fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", argN, argN+1)
	args = append(args, f.Limit, f.Offset)

	rows, err := r.pool.Query(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items, err := scanAuditRows(rows)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (r *Repository) GetAuditLog(ctx context.Context, id uuid.UUID) (*domain.AuditEntry, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, event_type, actor_id, actor_email, resource_type, resource_id,
			http_method, http_path, status_code, ip_address, user_agent, correlation_id, metadata, created_at
		FROM finance_audit_log WHERE id = $1
	`, id)

	var e domain.AuditEntry
	if err := scanAuditRow(row, &e); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &e, nil
}

func (r *Repository) MonitoringSummary(ctx context.Context) (*domain.MonitoringSummary, error) {
	summary := &domain.MonitoringSummary{
		Service:   "finance",
		CheckedAt: time.Now().UTC(),
	}

	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM chart_of_accounts WHERE active = TRUE`).Scan(&summary.ChartOfAccounts); err != nil {
		return nil, err
	}
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM journal_entries WHERE status = 'draft'`).Scan(&summary.JournalDraft); err != nil {
		return nil, err
	}
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM journal_entries WHERE status = 'posted'`).Scan(&summary.JournalPosted); err != nil {
		return nil, err
	}
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM ar_open_items WHERE status = 'open'`).Scan(&summary.AROpenItems); err != nil {
		return nil, err
	}
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM ap_open_items WHERE status = 'open'`).Scan(&summary.APOpenItems); err != nil {
		return nil, err
	}
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM processed_events`).Scan(&summary.ProcessedEvents); err != nil {
		return nil, err
	}
	if err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM finance_audit_log WHERE created_at >= NOW() - INTERVAL '24 hours'
	`).Scan(&summary.AuditLast24Hours); err != nil {
		return nil, err
	}
	if err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM finance_audit_log WHERE created_at >= NOW() - INTERVAL '1 hour'
	`).Scan(&summary.AuditLastHour); err != nil {
		return nil, err
	}
	if err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM finance_audit_log
		WHERE created_at >= NOW() - INTERVAL '24 hours' AND status_code >= 400
	`).Scan(&summary.HTTPErrorsLast24h); err != nil {
		return nil, err
	}

	summary.Integrations = []domain.Integration{
		{Name: "ura-efris", Status: "stub"},
		{Name: "banking", Status: "stub"},
		{Name: "kafka-consumer", Status: "disabled"},
	}

	return summary, nil
}

func scanAuditRows(rows pgx.Rows) ([]domain.AuditEntry, error) {
	var items []domain.AuditEntry
	for rows.Next() {
		var e domain.AuditEntry
		if err := scanAuditRow(rows, &e); err != nil {
			return nil, err
		}
		items = append(items, e)
	}
	return items, rows.Err()
}

type scannable interface {
	Scan(dest ...any) error
}

func scanAuditRow(row scannable, e *domain.AuditEntry) error {
	return row.Scan(
		&e.ID, &e.EventType, &e.ActorID, &e.ActorEmail, &e.ResourceType, &e.ResourceID,
		&e.HTTPMethod, &e.HTTPPath, &e.StatusCode, &e.IPAddress, &e.UserAgent, &e.CorrelationID,
		&e.Metadata, &e.CreatedAt,
	)
}
