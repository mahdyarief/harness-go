package dtos

import "hsg/internal/lib/validator"

// TopUpBody is the payload for granting tokens to the tenant named in the
// route. The tenant is never taken from the body.
type TopUpBody struct {
	Amount int64  `json:"amount"`
	Note   string `json:"note"`
}

func (dto TopUpBody) Validate(v *validator.MapValidator) {
	v.Field("amount").Required().Integer().Min(1)
	v.Field("note").String().MaxLen(maxTopUpNoteLen)
}

// maxTopUpNoteLen matches the column's CHECK constraint.
const maxTopUpNoteLen = 500

// TokenUsageResponse is the serialisable shape of one usage record.
type TokenUsageResponse struct {
	ID               string `json:"id"`
	TenantID         string `json:"tenant_id"`
	UserID           string `json:"user_id"`
	SessionID        string `json:"session_id,omitempty"`
	MessageID        string `json:"message_id,omitempty"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
	BilledTokens     int    `json:"billed_tokens"`
	CreatedAt        string `json:"created_at"`
}
