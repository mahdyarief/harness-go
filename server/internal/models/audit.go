package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// AuditLog is one immutable entry in the audit trail. It is intentionally
// generic: action and entity_type are free-form slugs so a new action never
// needs a migration, and the row keeps working after the entity it describes is
// deleted.
type AuditLog struct {
	ID         uuid.UUID       `db:"id" json:"id"`
	OccurredAt time.Time       `db:"occurred_at" json:"occurred_at"`
	ActorID    uuid.UUID       `db:"actor_id" json:"actor_id"`
	ActorRole  string          `db:"actor_role" json:"actor_role"`
	ActorName  string          `db:"actor_name" json:"actor_name,omitempty"`
	Action     string          `db:"action" json:"action"`
	EntityType string          `db:"entity_type" json:"entity_type"`
	EntityID   *uuid.UUID      `db:"entity_id" json:"entity_id"`
	TenantID   *uuid.UUID      `db:"tenant_id" json:"tenant_id"`
	RequestID  string          `db:"request_id" json:"request_id"`
	IP         string          `db:"ip" json:"ip"`
	UserAgent  string          `db:"user_agent" json:"user_agent"`
	Metadata   json.RawMessage `db:"metadata" json:"metadata"`
}
