package main

import (
	"github.com/gofiber/fiber/v3"

	"hsg/internal/app"
	"hsg/internal/handlers"
	"hsg/internal/middlewares"
)

// registerTokenRoutes exposes the token allocation API.
//
// Platform-only writes live under /platform so the SuperUser guard is obvious:
// granting tokens is an administrative act that affects billing. Reads are
// split by audience: a platform admin lists every tenant, while a tenant member
// reads their own tenant's balance and ledger. The repository re-checks the
// caller's role and membership on every read, so hiding a route is not the
// security boundary.
func registerTokenRoutes(v1 fiber.Router, app *app.Application) {
	h := handlers.New(app).Token
	m := middlewares.New(app)
	auth := m.Authorization
	idempotent := middlewares.Idempotency()

	platform := v1.Group("/platform/tokens", auth, m.SuperUser)
	platform.Get("/", h.ListBalances)
	platform.Post("/:tenant_id/topups", idempotent, h.TopUp)

	// Tenant-scoped reads: the caller must belong to the tenant (or be a
	// platform admin, which the repository also allows).
	tenant := v1.Group("/tenants/:tenant_id/tokens", auth)
	tenant.Get("/", h.GetBalance)
	tenant.Get("/topups", h.ListTopups)
	tenant.Get("/usages", h.ListUsages)
}
