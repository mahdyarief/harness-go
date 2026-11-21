package dtos

import "hsg/internal/lib/validator"

// KnowledgeStatusBody carries a lifecycle transition for a document.
type KnowledgeStatusBody struct {
	Status string `json:"status"`
}

func (dto KnowledgeStatusBody) Validate(v *validator.MapValidator) {
	v.Field("status").Required().Regex(`^(draft|published)$`)
}
