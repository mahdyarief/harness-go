package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/getsentry/sentry-go"
	sentryfiber "github.com/gofiber/contrib/v3/sentry"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/compress"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/helmet"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/gofiber/fiber/v3/middleware/requestid"

	"hsg/internal/app"
	"hsg/internal/handlers"
	"hsg/internal/middlewares"
	"hsg/internal/web"
)

// serve runs the HTTP server until SIGINT/SIGTERM, then drains in-flight
// requests before returning.
func serve(app *app.Application, knowledge *handlers.KnowledgeHandler) error {
	server, err := newServer(app, knowledge)
	if err != nil {
		return err
	}
	// A no-op unless newServer initialised Sentry.
	defer sentry.Flush(5 * time.Second)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app.Logger.Info("starting server", "port", app.Config.App.Port, "env", app.Config.App.Env, "auth", authBasePath)
	return server.Listen(fmt.Sprintf(":%d", app.Config.App.Port), fiber.ListenConfig{GracefulContext: ctx})
}

// newServer builds the Fiber app with its middleware and routes.
func newServer(app *app.Application, knowledge *handlers.KnowledgeHandler) (*fiber.App, error) {
	proxies := splitCSV(app.Config.App.TrustedProxies)

	server := fiber.New(fiber.Config{
		AppName:          app.Config.App.Name,
		ErrorHandler:     middlewares.ErrorHandler(app.Logger),
		TrustProxy:       len(proxies) > 0,
		TrustProxyConfig: fiber.TrustProxyConfig{Proxies: proxies},
		ProxyHeader:      fiber.HeaderXForwardedFor,
		// Without it c.IP() is the raw header, which a client prefixes with
		// whatever it likes, so every request gets a fresh rate-limit key.
		EnableIPValidation: true,
		// Bound how long a slow client can hold a connection. WriteTimeout is a
		// whole-response deadline that starts when the body begins streaming, so
		// it must exceed the agent's MaxDuration (90s): at 30s it severed long
		// tool-using chat turns mid-stream, dropping the answer and the citation
		// metering. ReadTimeout only covers the request bytes.
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  60 * time.Second,
		// Reject oversized request bodies before they are read into memory.
		// Fiber's own default is 4 MiB; keeping it explicit documents the cap.
		BodyLimit: 4 * 1024 * 1024,
	})

	// Middleware
	server.Use(recover.New())
	server.Use(logger.New())
	server.Use(helmet.New())
	server.Use(requestid.New())
	// Must follow requestid: it copies the correlation id plus the client
	// address and user agent onto the request context for the audit trail.
	server.Use(middlewares.RequestMeta)
	server.Use(compress.New())

	// Sentry reports panics, and only when a DSN is configured.
	if dsn := app.Config.Sentry.Dsn; dsn != "" {
		if err := sentry.Init(sentry.ClientOptions{Dsn: dsn, Environment: app.Config.App.Env}); err != nil {
			return nil, fmt.Errorf("init sentry: %w", err)
		}

		server.Use(sentryfiber.New(sentryfiber.Config{
			Repanic:         true,
			WaitForDelivery: true,
			Timeout:         5 * time.Second,
		}))
	}

	origins := splitCSV(app.Config.App.CORSAllowedOrigins)

	// CSRF: the session cookie is SameSite=Lax, which a sibling subdomain can
	// get around. This covers Authula's routes too, as they are mounted here.
	server.Use(middlewares.CrossOrigin(origins))

	// Authula answers CORS for its own routes from the same origin list, so
	// the Fiber middleware skips them to avoid duplicate headers.
	if len(origins) > 0 {
		server.Use(cors.New(cors.Config{
			Next:             func(c fiber.Ctx) bool { return strings.HasPrefix(c.Path(), authBasePath) },
			AllowOrigins:     origins,
			AllowCredentials: true,
			// Without this a browser client cannot read the marker that says a
			// write was replayed instead of applied (see middlewares.Idempotency).
			ExposeHeaders: []string{middlewares.HeaderIdempotentReplayed},
		}))
	}

	routes(server, app, knowledge)

	// The SPA is registered after the API so it only ever answers routes the
	// router did not match. A binary built without the web bundle still runs.
	spa, err := web.Handler()
	if err != nil {
		if !errors.Is(err, web.ErrNoBuild) {
			return nil, err
		}
		app.Logger.Warn("serving API only", "reason", err)
	} else {
		server.Use(spa)
	}

	return server, nil
}
