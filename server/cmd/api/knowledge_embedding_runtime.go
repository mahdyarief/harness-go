package main

import (
	"context"
	"fmt"
	"time"

	"hsg/internal/app"
	"hsg/internal/config"
	"hsg/internal/handlers"
	"hsg/internal/lib"
	"hsg/internal/lib/kbembed"
	"hsg/internal/models"
	"hsg/internal/repositories"
)

// Kebijakan embedding hanya berasal dari izin operator, bukan konfigurasi LLM.
type knowledgeEmbeddingClient struct {
	allowedHosts []string
	allowPrivate bool
}

var _ handlers.KnowledgeEmbeddingClient = knowledgeEmbeddingClient{}

func newKnowledgeEmbeddingClient(cfg config.Foundation) knowledgeEmbeddingClient {
	return knowledgeEmbeddingClient{
		allowedHosts: append([]string(nil), cfg.EmbeddingAllowedHosts...),
		allowPrivate: cfg.EmbeddingAllowPrivate,
	}
}

func (c knowledgeEmbeddingClient) Embed(ctx context.Context, cfg models.KnowledgeEmbeddingConfig, credential string, inputs []string) ([][]float32, error) {
	client, err := kbembed.New(kbembed.Config{
		Endpoint: cfg.Endpoint, Model: cfg.Model, Revision: cfg.Revision,
		Dimensions: cfg.Dimensions, Credential: credential,
		AllowedHosts: c.allowedHosts, AllowPrivate: c.allowPrivate,
		// CPU inference of a full 32-chunk batch can take ~45s; the default
		// 15s request timeout would abort every batch.
		Timeout: 120 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	return client.Embed(ctx, inputs)
}

func newKnowledgeEmbeddingProbe(application *app.Application) handlers.KnowledgeEmbeddingProbe {
	client := newKnowledgeEmbeddingClient(application.Foundation)
	return func(ctx context.Context, cfg models.KnowledgeEmbeddingConfig, credential string) error {
		_, err := client.Embed(ctx, cfg, credential, []string{"Uji koneksi embedding sintetis."})
		return err
	}
}

func startKnowledgeEmbeddingWorker(application *app.Application) (func(), error) {
	if application.Foundation.EmbeddingWorkerDSN == "" {
		return func() {}, nil
	}
	if len(application.Foundation.LLMEncryptionKey) != 32 {
		return nil, fmt.Errorf("worker embedding memerlukan kunci enkripsi credential 32 byte")
	}
	db, err := lib.OpenDB(application.Foundation.EmbeddingWorkerDSN)
	if err != nil {
		return nil, fmt.Errorf("koneksi database worker embedding gagal")
	}
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	db.SetConnMaxIdleTime(5 * time.Minute)
	repo := repositories.NewKnowledgeEmbeddingWorkerRepository(db)
	checkCtx, cancelCheck := context.WithTimeout(context.Background(), 5*time.Second)
	err = repo.ValidateRole(checkCtx)
	cancelCheck()
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("role database worker embedding harus non-owner dan hanya memiliki akses fungsi worker")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	client := newKnowledgeEmbeddingClient(application.Foundation)
	go func() {
		defer close(done)
		defer db.Close()
		if err := handlers.RunKnowledgeEmbeddingWorker(ctx, repo, application.Foundation.LLMEncryptionKey, client, application.Logger); err != nil {
			application.Logger.Error("worker embedding berhenti; akses database ditolak")
		}
	}()
	return func() {
		cancel()
		select {
		case <-done:
		case <-time.After(7 * time.Second):
			application.Logger.Warn("shutdown worker embedding melewati batas waktu")
		}
	}, nil
}
