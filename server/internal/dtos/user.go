package dtos

import (
	"hsg/internal/lib/validator"
)

const (
	maxUserNameLen = 100
	maxEmailLen    = 254
)

// emailRegex is deliberately loose: it only rejects obvious garbage before the
// address reaches Authula.
const emailRegex = `^[^@\s]+@[^@\s]+\.[^@\s]+$`

// roleRegex pins the platform role to the values the users.role column holds.
const roleRegex = `^(user|super_user)$`

// tenantRoleRegex pins the tenant role to the values the membership CHECK allows.
const tenantRoleRegex = `^(tenant_admin|reviewer)$`

// passwordRegex enforces the 12..128 window without lookaheads (RE2).
const passwordRegex = `^.{12,128}$`

const uuidRegex = `^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`

// Validate runs on the raw body before it is bound, so rules cannot depend on
// struct fields; the tenant_id/tenant_role pairing is checked by the handler.

// CreateUserBody is the super-user payload for creating a user with an
// admin-set password, a platform role, and an optional tenant membership.
type CreateUserBody struct {
	Name       string `json:"name"`
	Email      string `json:"email"`
	Password   string `json:"password"`
	Role       string `json:"role"`
	TenantID   string `json:"tenant_id"`
	TenantRole string `json:"tenant_role"`
}

func (CreateUserBody) Validate(v *validator.MapValidator) {
	v.Field("name").Required().String().MaxLen(maxUserNameLen)
	v.Field("email").Required().String().MaxLen(maxEmailLen).Regex(emailRegex)
	v.Field("password").Required().String().Regex(passwordRegex)
	v.Field("role").Required().Regex(roleRegex)
	v.Field("tenant_id").Regex(uuidRegex)
	v.Field("tenant_role").Regex(tenantRoleRegex)
}

// UpdateUserBody edits a user. An absent password leaves credentials alone;
// an absent tenant leaves memberships alone.
type UpdateUserBody struct {
	Name       string `json:"name"`
	Email      string `json:"email"`
	Password   string `json:"password"`
	Role       string `json:"role"`
	TenantID   string `json:"tenant_id"`
	TenantRole string `json:"tenant_role"`
}

func (UpdateUserBody) Validate(v *validator.MapValidator) {
	v.Field("name").Required().String().MaxLen(maxUserNameLen)
	v.Field("email").Required().String().MaxLen(maxEmailLen).Regex(emailRegex)
	v.Field("password").String().Regex(passwordRegex)
	v.Field("role").Required().Regex(roleRegex)
	v.Field("tenant_id").Regex(uuidRegex)
	v.Field("tenant_role").Regex(tenantRoleRegex)
}
