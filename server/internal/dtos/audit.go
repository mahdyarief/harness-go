package dtos

import "hsg/internal/lib/validator"

// AuditQuery filters the audit trail. Every field is optional; absent filters
// widen the result rather than failing. The UUID and time rules reject a
// malformed value up front so the repository never has to parse request input.
type AuditQuery struct {
	ListQuery
	Action     string `query:"action"`
	EntityType string `query:"entity_type"`
	EntityID   string `query:"entity_id"`
	ActorID    string `query:"actor_id"`
	TenantID   string `query:"tenant_id"`
	From       string `query:"from"`
	To         string `query:"to"`
}

func (dto AuditQuery) Validate(v *validator.MapValidator) {
	dto.ListQuery.base(v)
	v.Field("action").String().MaxLen(maxAuditSlugLen)
	v.Field("entity_type").String().MaxLen(maxAuditSlugLen)
	v.Field("entity_id").UUID()
	v.Field("actor_id").UUID()
	v.Field("tenant_id").UUID()
	v.Field("from").Regex(auditTimePattern)
	v.Field("to").Regex(auditTimePattern)
}

const (
	// maxAuditSlugLen mirrors the column CHECK on action/entity_type.
	maxAuditSlugLen = 100
	// auditTimePattern accepts a plain date or a full RFC3339 timestamp.
	auditTimePattern = `^\d{4}-\d{2}-\d{2}(T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2}))?$`
)
