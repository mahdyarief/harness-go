package lib

import (
	"context"

	"github.com/google/uuid"
)

type tenantKey struct{}

// WithTenant attaches the caller's tenant to the context. Repositories that
// pin row-level-security via app.tenant_id read it back from ctx instead of
// doing a cross-tenant lookup by row id.
func WithTenant(ctx context.Context, tenant uuid.UUID) context.Context {
	return context.WithValue(ctx, tenantKey{}, tenant)
}

// TenantFrom returns the tenant stored on ctx, or false when the context has
// no tenant (tests, background jobs, platform calls).
func TenantFrom(ctx context.Context) (uuid.UUID, bool) {
	tenant, ok := ctx.Value(tenantKey{}).(uuid.UUID)
	return tenant, ok
}
