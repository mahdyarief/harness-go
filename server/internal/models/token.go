package models

import (
	"time"

	"github.com/google/uuid"
)

// TokenBalance is a tenant's current token pool. It holds the only mutable
// value in the feature: the balance is spent down by LLM calls and only ever
// increased by an admin top-up.
type TokenBalance struct {
	TenantID uuid.UUID `db:"tenant_id" json:"tenant_id"`
	// TenantName is filled only by the platform list, which joins tenants so an
	// admin can tell rows apart without a second request.
	TenantName string    `db:"tenant_name" json:"tenant_name,omitempty"`
	Balance    int64     `db:"balance" json:"balance"`
	UpdatedAt  time.Time `db:"updated_at" json:"updated_at"`
}

// TokenTopup is one addition to a tenant's pool. The ledger is append-only so
// the history of who granted what, and why, stays intact.
type TokenTopup struct {
	ID         uuid.UUID `db:"id" json:"id"`
	TenantID   uuid.UUID `db:"tenant_id" json:"tenant_id"`
	Amount     int64     `db:"amount" json:"amount"`
	Note       string    `db:"note" json:"note"`
	ActorID    uuid.UUID `db:"actor_id" json:"actor_id"`
	ActorName  string    `db:"actor_name" json:"actor_name"`
	ActorEmail string    `db:"actor_email" json:"actor_email"`
	Reason     string    `db:"reason" json:"reason"`
	CreatedAt  time.Time `db:"created_at" json:"created_at"`
}

// TokenUsage is one deduction: the tokens a single completed agent turn spent.
// It is append-only and references the session and message that caused it, so
// usage is traceable back to the exact request for debugging.
type TokenUsage struct {
	ID               uuid.UUID  `db:"id" json:"id"`
	TenantID         uuid.UUID  `db:"tenant_id" json:"tenant_id"`
	UserID           uuid.UUID  `db:"user_id" json:"user_id"`
	UserName         string     `db:"user_name" json:"user_name"`
	UserEmail        string     `db:"user_email" json:"user_email"`
	SessionID        *uuid.UUID `db:"session_id" json:"session_id"`
	MessageID        *uuid.UUID `db:"message_id" json:"message_id"`
	PromptTokens     int        `db:"prompt_tokens" json:"prompt_tokens"`
	CompletionTokens int        `db:"completion_tokens" json:"completion_tokens"`
	TotalTokens      int        `db:"total_tokens" json:"total_tokens"`
	// BilledTokens is what was actually deducted; it differs from TotalTokens
	// only on the final turn before exhaustion, when the charge is clamped to
	// the remaining balance.
	BilledTokens int       `db:"billed_tokens" json:"billed_tokens"`
	CreatedAt    time.Time `db:"created_at" json:"created_at"`
}
