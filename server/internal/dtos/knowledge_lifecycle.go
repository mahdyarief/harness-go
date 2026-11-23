package dtos

import (
	"encoding/json"

	"hsg/internal/lib/validator"
)

// KnowledgeStatusBody carries a lifecycle transition for a document.
type KnowledgeStatusBody struct {
	Status string `json:"status"`
}

func (dto KnowledgeStatusBody) Validate(v *validator.MapValidator) {
	v.Field("status").Required().Regex(`^(draft|published)$`)
}

// KnowledgeMetadataBody updates the retrieval metadata of one document.
// Omitted fields stay untouched; an explicit empty date clears that bound and
// an empty attributes object clears the attribute set.
type KnowledgeMetadataBody struct {
	Attributes json.RawMessage `json:"attributes"`
	ValidFrom  *string         `json:"valid_from"`
	ValidTo    *string         `json:"valid_to"`
	Version    *string         `json:"version"`
}

// Validate leaves the fields optional; the handler checks the date window and
// the attribute shape because both need cross-field rules.
func (dto KnowledgeMetadataBody) Validate(*validator.MapValidator) {}
