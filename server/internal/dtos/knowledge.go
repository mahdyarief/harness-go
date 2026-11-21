package dtos

import (
	"github.com/google/uuid"

	"hsg/internal/lib/validator"
)

// KnowledgeQuery lists knowledge documents. The scope (global or one tenant)
// comes from the route, never from the client. CategoryID and Uncategorized
// are facets the caller may use to narrow the list.
type KnowledgeQuery struct {
	ListQuery
	CategoryID    *uuid.UUID `query:"category_id"`
	Uncategorized bool       `query:"uncategorized"`
}

func (dto KnowledgeQuery) Validate(v *validator.MapValidator) {
	dto.ListQuery.base(v)
	v.Field("category_id").UUID()
	v.Field("uncategorized").Regex(`^(true|false)?$`)
}

type KnowledgePreviewQuery struct {
	ListQuery
}

func (dto KnowledgePreviewQuery) Validate(v *validator.MapValidator) {
	v.Field("offset").Regex(`^\d+$`)
	v.Field("limit").Regex(`^\d+$`)
}

func (dto *KnowledgePreviewQuery) Clamp() {
	dto.ListQuery.Clamp()
	if dto.Limit > 20 {
		dto.Limit = 20
	}
}

// MaxKnowledgeTitleLen mirrors the column CHECK on knowledge_documents.title.
const MaxKnowledgeTitleLen = 200
