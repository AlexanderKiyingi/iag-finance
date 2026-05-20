package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type AuditEntry struct {
	ID             uuid.UUID       `json:"id"`
	EventType      string          `json:"eventType"`
	ActorID        *uuid.UUID      `json:"actorId,omitempty"`
	ActorEmail     string          `json:"actorEmail"`
	ResourceType   *string         `json:"resourceType,omitempty"`
	ResourceID     *string         `json:"resourceId,omitempty"`
	HTTPMethod     *string         `json:"httpMethod,omitempty"`
	HTTPPath       *string         `json:"httpPath,omitempty"`
	StatusCode     *int            `json:"statusCode,omitempty"`
	IPAddress      *string         `json:"ipAddress,omitempty"`
	UserAgent      *string         `json:"userAgent,omitempty"`
	CorrelationID  *string         `json:"correlationId,omitempty"`
	Metadata       json.RawMessage `json:"metadata"`
	CreatedAt      time.Time       `json:"createdAt"`
}
