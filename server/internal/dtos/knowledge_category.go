package dtos

import (
	"github.com/google/uuid"

	"hsg/internal/lib/validator"
)

// KnowledgeCategoryBody is the create/rename payload for a category. The
// repository folds the name into a slug, so the client never sends one.
type KnowledgeCategoryBody struct {
	Name string `json:"name"`
}

func (dto KnowledgeCategoryBody) Validate(v *validator.MapValidator) {
	v.Field("name").Required().String().MaxLen(80)
}

// KnowledgeCategoryQuery extends the shared pagination with an optional
// "uncategorized" flag that matches documents with no category.
type KnowledgeCategoryQuery struct {
	ListQuery
	Uncategorized bool `query:"uncategorized"`
}

func (dto KnowledgeCategoryQuery) Validate(v *validator.MapValidator) {
	dto.base(v)
	v.Field("uncategorized").Regex(`^(true|false)?$`)
}

// KnowledgeCategoryAssignBody carries the category a document is moved to.
// CategoryID is nil when the document is returned to "Tanpa kategori".
type KnowledgeCategoryAssignBody struct {
	CategoryID *uuid.UUID `json:"category_id"`
}

func (dto KnowledgeCategoryAssignBody) Validate(v *validator.MapValidator) {
	v.Field("category_id").UUID()
}
