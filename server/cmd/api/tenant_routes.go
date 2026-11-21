package main

import (
	"github.com/gofiber/fiber/v3"
	"hsg/internal/app"
	"hsg/internal/handlers"
	"hsg/internal/middlewares"
)

func registerTenantRoutes(v1 fiber.Router, app *app.Application) {
	h := handlers.NewTenantHandler(app)
	m := middlewares.New(app)
	auth := m.Authorization
	v1.Get("/tenants", auth, h.List)
	v1.Post("/platform/tenants", auth, h.Create)
	// The platform directory: every tenant, for the user drawer's picker.
	v1.Get("/platform/tenants", auth, m.SuperUser, h.PlatformList)
	// Every tenant grouped with its members, for the platform tenant page.
	v1.Get("/platform/tenants/directory", auth, m.SuperUser, h.PlatformDirectory)
	v1.Get("/tenants/:tenant_id/members", auth, h.Members)
	v1.Put("/tenants/:tenant_id/members", auth, h.AssignMember)
}
