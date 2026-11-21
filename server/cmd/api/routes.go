package main

import (
	fiberadapter "github.com/Authula/authula/adapters/fiber"
	"github.com/gofiber/fiber/v3"

	"hsg/internal/app"
	"hsg/internal/config"
	"hsg/internal/handlers"
	"hsg/internal/middlewares"
)

// Per-IP request budgets per minute: tight on the auth routes, loose on the
// rest of the API.
const (
	authRateLimit = 30
	apiRateLimit  = 120
)

func routes(r *fiber.App, app *app.Application, knowledge *handlers.KnowledgeHandler) {
	h := handlers.New(app)
	m := middlewares.New(app)

	r.Get("/health", h.Health.Check)

	// Authula: email & password, Google OAuth2 and magic-link sign-in,
	// plus GET /me and POST /sign-out, all under authBasePath. The limiter
	// guards password guessing and sign-in/reset email flooding.
	r.Use(authBasePath,
		middlewares.RateLimit(authRateLimit, app.Config.App.Env == config.EnvDevelopment, splitCSV(app.Config.App.RateLimitExemptIPs)),
		fiberadapter.New(fiberadapter.Config{Handler: app.Auth.Handler()}),
	)

	exemptLoopback := app.Config.App.Env == config.EnvDevelopment
	exemptIPs := splitCSV(app.Config.App.RateLimitExemptIPs)

	// ponytail: the limiters count in memory, so each replica has its own
	// budget; give them shared storage once there is more than one instance.
	v1 := r.Group("/api/v1", middlewares.RateLimit(apiRateLimit, exemptLoopback, exemptIPs))
	registerTenantRoutes(v1, app)
	registerChatRoutes(v1, app)
	registerTokenRoutes(v1, app)
	registerAuditRoutes(v1, app)
	registerKnowledgeEmbeddingConfigRoutes(v1, app, newKnowledgeEmbeddingProbe(app))
	registerKnowledgeRoutes(v1, app, knowledge)
	registerKnowledgeEmbeddingStatusRoutes(v1, app)

	// Reads are public; writes need a signed-in super user. Idempotency comes
	// last, after the caller is known, and replays a repeated write instead of
	// applying it twice (see middlewares.Idempotency). One guard serves every
	// write route: its keys are already scoped per route, and a shared cache
	// means a single in-memory store instead of one per route.
	idempotent := middlewares.Idempotency()

	// The application profile: Authula knows the user, hsg knows the role.
	v1.Get("/me", m.Authorization, h.Me.Get)

	// The user directory backs the platform admin's pickers (e.g. choosing a
	// tenant's first admin), so it is super-user only. The write routes do
	// more than the mirror: they provision the Authula account and the tenant
	// membership, so they carry the same guard plus idempotency.
	v1.Get("/users", m.Authorization, m.SuperUser, h.User.List)
	v1.Get("/users/:id/membership", m.Authorization, m.SuperUser, h.User.Membership)
	v1.Post("/users", m.Authorization, m.SuperUser, idempotent, h.User.Create)
	v1.Put("/users/:id", m.Authorization, m.SuperUser, idempotent, h.User.Update)

	// Provider configuration is global and restricted to platform super users.
	llm := v1.Group("/platform", m.Authorization, m.SuperUser)
	llm.Get("/llm-config", h.LLM.Get)
	llm.Put("/llm-config", h.LLM.Put)
	llm.Post("/llm-test", h.LLM.Test)
	llm.Get("/exa-config", h.Exa.Get)
	llm.Put("/exa-config", h.Exa.Put)
	llm.Post("/exa-test", h.Exa.Test)

	categories := v1.Group("/categories")
	categories.Get("/", h.Category.List)
	categories.Get("/:id", h.Category.Get)
	categories.Post("/", m.Authorization, m.SuperUser, idempotent, h.Category.Create)
	categories.Put("/:id", m.Authorization, m.SuperUser, idempotent, h.Category.Update)
	categories.Delete("/:id", m.Authorization, m.SuperUser, idempotent, h.Category.Delete)
	categories.Put("/:id/restore", m.Authorization, m.SuperUser, idempotent, h.Category.Restore)
	categories.Delete("/:id/force", m.Authorization, m.SuperUser, idempotent, h.Category.DeletePermanently)
}
