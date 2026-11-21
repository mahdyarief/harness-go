package models

import (
	"github.com/google/uuid"
	"time"
)

type Tenant struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Role      string    `json:"role,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}
type TenantMembership struct {
	TenantID  uuid.UUID `json:"tenant_id"`
	UserID    uuid.UUID `json:"user_id"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

// TenantMember is one membership enriched with the user's identity, for the
// tenant member list. The users table has no RLS, so the identity is joined
// inside the same transaction as the membership check.
type TenantMember struct {
	UserID    uuid.UUID `json:"user_id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

// PlatformTenantMember is a membership enriched with the user's identity, for
// the platform admin's tenant directory.
type PlatformTenantMember struct {
	UserID    uuid.UUID `json:"user_id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

// PlatformTenantGroup is one tenant together with its members. A tenant with
// no members is still returned, with an empty list.
type PlatformTenantGroup struct {
	ID        uuid.UUID              `json:"id"`
	Name      string                 `json:"name"`
	CreatedAt time.Time              `json:"created_at"`
	Members   []PlatformTenantMember `json:"members"`
}
