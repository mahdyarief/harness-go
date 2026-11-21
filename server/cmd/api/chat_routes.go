package main

import (
	"github.com/gofiber/fiber/v3"
	"hsg/internal/app"
	"hsg/internal/handlers"
	"hsg/internal/middlewares"
)

func registerChatRoutes(v1 fiber.Router, app *app.Application) {
	h := handlers.NewChatHandler(app)
	auth := middlewares.New(app).Authorization
	idempotent := middlewares.Idempotency()

	v1.Get("/chat/access", auth, h.Access)

	sessions := v1.Group("/chat/sessions", auth)
	sessions.Get("/", h.ListSessions)
	sessions.Post("/", idempotent, h.CreateSession)
	sessions.Get("/:session_id", h.GetSession)
	sessions.Get("/:session_id/messages", h.Messages)
	sessions.Get("/:session_id/tool-logs", h.ToolLogs)
	sessions.Delete("/:session_id", h.DeleteSession)
	sessions.Put("/:session_id", idempotent, h.RenameSession)
	sessions.Post("/:session_id/turn", h.Turn)
}
