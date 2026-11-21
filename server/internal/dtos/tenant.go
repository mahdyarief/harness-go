package dtos

import "github.com/google/uuid"

type CreateTenantBody struct {
	Name        string    `json:"name"`
	AdminUserID uuid.UUID `json:"admin_user_id"`
}
type TenantMemberBody struct {
	UserID uuid.UUID `json:"user_id"`
	Role   string    `json:"role"`
}
