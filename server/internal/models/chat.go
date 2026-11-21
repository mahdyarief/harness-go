package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ChatScope is selected server-side from the current DB role, never client input.
type ChatScope struct {
	Kind     string    `json:"kind"`
	TenantID uuid.UUID `json:"-"`
}

const ChatPlatformPersonal = "platform-personal"
const ChatTenantPersonal = "tenant-personal"

func (s ChatScope) Platform() bool { return s.Kind == ChatPlatformPersonal }
func (s ChatScope) DatabaseTenantID() *uuid.UUID {
	if s.Platform() {
		return nil
	}
	id := s.TenantID
	return &id
}

// ChatSession is one user's conversation thread.
type ChatSession struct {
	ID        uuid.UUID  `db:"id" json:"id"`
	TenantID  *uuid.UUID `db:"tenant_id" json:"tenant_id"`
	Scope     string     `json:"scope"`
	UserID    uuid.UUID  `db:"user_id" json:"user_id"`
	Title     string     `db:"title" json:"title"`
	CreatedAt time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt time.Time  `db:"updated_at" json:"updated_at"`
}

// ChatMessage is one turn in a session.
type ChatMessage struct {
	ID           uuid.UUID       `db:"id" json:"id"`
	SessionID    uuid.UUID       `db:"session_id" json:"session_id"`
	Role         string          `db:"role" json:"role"`
	Content      string          `db:"content" json:"content"`
	ToolCallID   *string         `db:"tool_call_id" json:"tool_call_id,omitempty"`
	ToolName     *string         `db:"tool_name" json:"tool_name,omitempty"`
	FinishReason *string         `db:"finish_reason" json:"finish_reason,omitempty"`
	References   json.RawMessage `db:"references" json:"references"`
	CreatedAt    time.Time       `db:"created_at" json:"created_at"`
}

// ChatTokenUsage records what the provider reported for one assistant turn.
type ChatTokenUsage struct {
	ID               uuid.UUID `db:"id" json:"id"`
	SessionID        uuid.UUID `db:"session_id" json:"session_id"`
	MessageID        uuid.UUID `db:"message_id" json:"message_id"`
	PromptTokens     int       `db:"prompt_tokens" json:"prompt_tokens"`
	CompletionTokens int       `db:"completion_tokens" json:"completion_tokens"`
	TotalTokens      int       `db:"total_tokens" json:"total_tokens"`
	CreatedAt        time.Time `db:"created_at" json:"created_at"`
}

// ChatToolLog records one tool invocation for audit.
type ChatToolLog struct {
	ID         uuid.UUID `db:"id" json:"id"`
	SessionID  uuid.UUID `db:"session_id" json:"session_id"`
	Turn       int       `db:"turn" json:"turn"`
	ToolName   string    `db:"tool_name" json:"tool_name"`
	Arguments  string    `db:"arguments" json:"arguments"`
	Result     string    `db:"result" json:"result"`
	DurationMs int       `db:"duration_ms" json:"duration_ms"`
	Error      *string   `db:"error" json:"error,omitempty"`
	CreatedAt  time.Time `db:"created_at" json:"created_at"`
}
