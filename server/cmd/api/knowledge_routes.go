package main

import (
	"github.com/gofiber/fiber/v3"

	"hsg/internal/app"
	"hsg/internal/handlers"
	"hsg/internal/middlewares"
	"hsg/internal/repositories"
)

func registerKnowledgeRoutes(v1 fiber.Router, a *app.Application, h *handlers.KnowledgeHandler) {
	m := middlewares.New(a)
	idempotent := middlewares.Idempotency()
	usage := handlers.NewKnowledgeUsageHandler(repositories.NewKnowledgeUsageRepository(a.TenantDB))
	v1.Get("/platform/knowledge/:id/preview", m.Authorization, m.SuperUser, h.Preview)
	v1.Get("/tenants/:tenant_id/knowledge/:id/preview", m.Authorization, h.Preview)
	v1.Get("/platform/knowledge", m.Authorization, m.SuperUser, h.List)
	v1.Post("/platform/knowledge", m.Authorization, m.SuperUser, idempotent, h.Upload)
	v1.Delete("/platform/knowledge/:id", m.Authorization, m.SuperUser, idempotent, h.Delete)
	v1.Get("/platform/knowledge/metrics", m.Authorization, m.SuperUser, usage.Window)
	v1.Get("/platform/knowledge/:id/metrics", m.Authorization, m.SuperUser, usage.Document)
	v1.Get("/tenants/:tenant_id/knowledge", m.Authorization, h.List)
	v1.Post("/tenants/:tenant_id/knowledge", m.Authorization, idempotent, h.Upload)
	v1.Delete("/tenants/:tenant_id/knowledge/:id", m.Authorization, idempotent, h.Delete)
	v1.Get("/tenants/:tenant_id/knowledge/metrics", m.Authorization, usage.Window)
	v1.Get("/tenants/:tenant_id/knowledge/:id/metrics", m.Authorization, usage.Document)

	v1.Get("/platform/knowledge/categories", m.Authorization, m.SuperUser, h.ListCategories)
	v1.Post("/platform/knowledge/categories", m.Authorization, m.SuperUser, h.CreateCategory)
	v1.Patch("/platform/knowledge/categories/:id", m.Authorization, m.SuperUser, h.RenameCategory)
	v1.Delete("/platform/knowledge/categories/:id", m.Authorization, m.SuperUser, h.DeleteCategory)
	v1.Patch("/platform/knowledge/:id/category", m.Authorization, m.SuperUser, h.SetCategory)
	v1.Patch("/platform/knowledge/:id/status", m.Authorization, m.SuperUser, h.SetStatus)
	v1.Patch("/platform/knowledge/:id/metadata", m.Authorization, m.SuperUser, h.SetMetadata)
	v1.Get("/tenants/:tenant_id/knowledge/categories", m.Authorization, h.ListCategories)
	v1.Post("/tenants/:tenant_id/knowledge/categories", m.Authorization, h.CreateCategory)
	v1.Patch("/tenants/:tenant_id/knowledge/categories/:id", m.Authorization, h.RenameCategory)
	v1.Delete("/tenants/:tenant_id/knowledge/categories/:id", m.Authorization, h.DeleteCategory)
	v1.Patch("/tenants/:tenant_id/knowledge/:id/category", m.Authorization, h.SetCategory)
	v1.Patch("/tenants/:tenant_id/knowledge/:id/status", m.Authorization, h.SetStatus)
	v1.Patch("/tenants/:tenant_id/knowledge/:id/metadata", m.Authorization, h.SetMetadata)
}
