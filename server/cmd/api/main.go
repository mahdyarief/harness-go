package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	"hsg/internal/app"
	"hsg/internal/config"
	"hsg/internal/handlers"
	"hsg/internal/lib/blob"
	"hsg/internal/lib/kb"
	"hsg/internal/repositories"
	"hsg/internal/seeders"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	var cfg config.Config
	parseFlag(&cfg)
	foundation, err := readFoundationEnv()
	if err != nil {
		return err
	}

	level := slog.LevelInfo
	if cfg.App.Debug {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	// The repositories log their queries through the default logger.
	slog.SetDefault(logger)

	if cfg.App.MigrateOnBoot {
		if err := migrateUp(&cfg.DB, logger); err != nil {
			return err
		}
	}

	tenantDB, err := connectTenantDB(foundation.TenantDSN)
	if err != nil {
		return err
	}
	defer tenantDB.Close()

	apiDB := cfg.DB
	apiDB.MaxOpenConns, apiDB.MaxIdleConns = apiPoolSize(cfg.DB.MaxOpenConns), apiPoolSize(cfg.DB.MaxIdleConns)
	db, err := connectDB(&apiDB)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer db.Close()

	repos := repositories.New(db)
	mirror := mirrorUser(&repos.User)

	// Creating the instance runs Authula's own migrations.
	auth := newAuth(cfg, mirror, logger)
	defer auth.CloseSystems()
	defer auth.ClosePlugins()

	// Repairs mirror rows a failed hook left missing or stale.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	err = repos.User.Reconcile(ctx)
	cancel()
	if err != nil {
		return fmt.Errorf("reconcile users mirror: %w", err)
	}

	// Seeding follows Authula because the super user is created through it.
	if cfg.App.SeedOnBoot {
		superUser := seeders.SuperUserSeeder{
			DB:       db,
			Auth:     auth.CoreServices(),
			Promote:  promoteSuperUser(&repos.User, mirror),
			Email:    cfg.App.SuperUserEmail,
			Password: cfg.App.SuperUserPassword,
		}
		if err := seedAll(db, superUser, logger); err != nil {
			return err
		}
	}

	store, err := blob.NewDisk(cfg.App.KBStorageDir)
	if err != nil {
		return fmt.Errorf("open knowledge storage: %w", err)
	}
	defer store.Close()
	cleanupCtx, cancelCleanup := context.WithCancel(context.Background())
	cleanupDone := make(chan struct{})
	go func() {
		defer close(cleanupDone)
		kb.RunCleanup(cleanupCtx, repositories.NewKnowledgeCleanupRepository(db), store, time.Minute, logger)
	}()
	defer func() {
		cancelCleanup()
		select {
		case <-cleanupDone:
		case <-time.After(5 * time.Second):
			logger.Warn("knowledge blob cleanup shutdown timed out")
		}
	}()
	extractors := kb.NewRegistry(kb.NewMarkdownExtractor(), kb.NewTextExtractor(), kb.NewCSVExtractor(), kb.NewDOCXExtractor(), kb.NewXLSXExtractor(), kb.NewPDFExtractor())

	application := &app.Application{
		Config:       cfg,
		Foundation:   foundation,
		TenantDB:     tenantDB,
		Logger:       logger,
		DB:           db,
		Repositories: repos,
		Auth:         auth,
	}
	stopEmbeddingWorker, err := startKnowledgeEmbeddingWorker(application)
	if err != nil {
		return err
	}
	defer stopEmbeddingWorker()
	return serve(application, handlers.NewKnowledgeHandler(application, store, extractors))
}
