package auditlog

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/iag-finance/backend/internal/domain"
	"github.com/iag-finance/backend/internal/repository"
)

type RecordInput struct {
	EventType     string
	ActorID       *uuid.UUID
	ActorEmail    string
	ResourceType  string
	ResourceID    string
	HTTPMethod    string
	HTTPPath      string
	StatusCode    int
	IPAddress     string
	UserAgent     string
	CorrelationID string
	Metadata      map[string]any
}

type ListFilter struct {
	EventType  string
	ActorID    *uuid.UUID
	Resource   string
	From       *time.Time
	To         *time.Time
	Limit      int
	Offset     int
}

type Service struct {
	repo *repository.Repository
}

func New(repo *repository.Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Record(ctx context.Context, in RecordInput) error {
	return s.repo.InsertAuditLog(ctx, repository.AuditLogParams{
		EventType:     in.EventType,
		ActorID:       in.ActorID,
		ActorEmail:    in.ActorEmail,
		ResourceType:  optionalString(in.ResourceType),
		ResourceID:    optionalString(in.ResourceID),
		HTTPMethod:    optionalString(in.HTTPMethod),
		HTTPPath:      optionalString(in.HTTPPath),
		StatusCode:    optionalInt(in.StatusCode),
		IPAddress:     optionalString(in.IPAddress),
		UserAgent:     optionalString(in.UserAgent),
		CorrelationID: optionalString(in.CorrelationID),
		Metadata:      in.Metadata,
	})
}

func (s *Service) List(ctx context.Context, f ListFilter) ([]domain.AuditEntry, int, error) {
	return s.repo.ListAuditLogs(ctx, repository.AuditListFilter{
		EventType: f.EventType,
		ActorID:   f.ActorID,
		Resource:  f.Resource,
		From:      f.From,
		To:        f.To,
		Limit:     f.Limit,
		Offset:    f.Offset,
	})
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*domain.AuditEntry, error) {
	return s.repo.GetAuditLog(ctx, id)
}

func (s *Service) MonitoringSummary(ctx context.Context) (*domain.MonitoringSummary, error) {
	return s.repo.MonitoringSummary(ctx)
}

func (s *Service) RecentActivity(ctx context.Context, limit int) ([]domain.AuditEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 25
	}
	items, _, err := s.List(ctx, ListFilter{Limit: limit, Offset: 0})
	return items, err
}

func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func optionalInt(n int) *int {
	if n == 0 {
		return nil
	}
	return &n
}
