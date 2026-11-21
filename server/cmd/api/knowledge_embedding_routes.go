package main

import (
	"github.com/gofiber/fiber/v3"
	"hsg/internal/app"
	"hsg/internal/handlers"
	"hsg/internal/middlewares"
	"hsg/internal/repositories"
)

// The lead wires the concrete allow-listed kbembed probe; nil fails closed on enable/test.
func registerKnowledgeEmbeddingConfigRoutes(v1 fiber.Router, a *app.Application, probe handlers.KnowledgeEmbeddingProbe) {
	m := middlewares.New(a)
	h := handlers.NewKnowledgeEmbeddingHandler(repositories.NewKnowledgeEmbeddingRepository(a.TenantDB), a.Foundation.LLMEncryptionKey, probe)
	platform := v1.Group("/platform/knowledge", m.Authorization, m.SuperUser)
	platform.Get("/embedding-config", h.Get)
	platform.Put("/embedding-config", h.Update)
	platform.Post("/embedding-test", h.Test)
}

func registerKnowledgeEmbeddingStatusRoutes(v1 fiber.Router, a *app.Application) {
	m := middlewares.New(a)
	h := handlers.NewKnowledgeEmbeddingHandler(repositories.NewKnowledgeEmbeddingRepository(a.TenantDB), nil, nil)
	platform := v1.Group("/platform/knowledge", m.Authorization, m.SuperUser)
	platform.Get("/:id/indexing", h.Indexing)
	platform.Post("/:id/reindex", h.Retry)
	tenant := v1.Group("/tenants/:tenant_id/knowledge", m.Authorization)
	tenant.Get("/:id/indexing", h.Indexing)
	tenant.Post("/:id/reindex", h.Retry)
}
