package main

import (
	"github.com/gofiber/fiber/v3"
	"hsg/internal/app"
	"hsg/internal/handlers"
	"hsg/internal/middlewares"
)

// registerReviewRoutes exposes the tenant review inbox. Reads and decisions
// are tenant-scoped: the handler re-proves the caller's membership and the
// store pins the tenant for RLS, so the route only needs authentication.
func registerReviewRoutes(v1 fiber.Router, app *app.Application) {
	h := handlers.NewReviewHandler(app)
	m := middlewares.New(app)
	auth := m.Authorization

	reviews := v1.Group("/tenants/:tenant_id/reviews", auth)
	reviews.Get("/", h.List)
	reviews.Get("/:id", h.Get)
	reviews.Post("/:id/decisions", h.Decide)
}
