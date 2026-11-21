package main

import (
	"github.com/gofiber/fiber/v3"

	"hsg/internal/app"
	"hsg/internal/handlers"
	"hsg/internal/middlewares"
)

// registerAuditRoutes exposes the read side of the audit trail. It is
// platform-only: an audit trail spans every tenant, so tenant members must not
// see other tenants' activity. The repository re-proves the super-user role in
// the transaction, so hiding the route is not the security boundary.
func registerAuditRoutes(v1 fiber.Router, app *app.Application) {
	h := handlers.New(app).Audit
	m := middlewares.New(app)

	audit := v1.Group("/platform/audit-logs", m.Authorization, m.SuperUser)
	audit.Get("/", h.List)
	audit.Get("/:id", h.Get)
}
