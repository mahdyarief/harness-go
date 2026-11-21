package handlers

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	"hsg/internal/lib/apperr"
	"hsg/internal/models"
	"hsg/internal/repositories"
)

// Embed implements only dense, normalized 1024-dimensional vectors; no provider fallback.
type KnowledgeEmbeddingClient interface {
	Embed(context.Context, models.KnowledgeEmbeddingConfig, string, []string) ([][]float32, error)
}

func RunKnowledgeEmbeddingWorker(ctx context.Context, repo *repositories.KnowledgeEmbeddingWorkerRepository, key []byte, client KnowledgeEmbeddingClient, logger *slog.Logger) error {
	if client == nil {
		return errors.New("embedding client unavailable")
	}
	if err := repo.ValidateRole(ctx); err != nil {
		return err
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
		job, err := repo.Claim(ctx)
		if errors.Is(err, apperr.ErrNotFound) {
			timer.Reset(2 * time.Second)
			continue
		}
		// translateNotFound returns a fresh *apperr.Error with KindNotFound,
		// so a sentinel comparison misses it; unwrap via errors.As instead.
		var ae *apperr.Error
		if errors.As(err, &ae) && ae.Kind == apperr.KindNotFound {
			timer.Reset(2 * time.Second)
			continue
		}
		if err != nil {
			logger.Warn("embedding job claim failed", "error", err)
			timer.Reset(5 * time.Second)
			continue
		}
		err = runKnowledgeEmbeddingBatch(ctx, repo, key, client, job)
		if err != nil {
			failCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			_ = repo.Fail(failCtx, job, false)
			cancel()
			logger.Warn("embedding batch failed")
		}
		timer.Reset(0)
	}
}

func runKnowledgeEmbeddingBatch(ctx context.Context, repo *repositories.KnowledgeEmbeddingWorkerRepository, key []byte, client KnowledgeEmbeddingClient, job repositories.KnowledgeEmbeddingJob) error {
	// Above the 120s embedding request timeout: the batch context must not
	// cancel a still-running CPU inference before the client gives up.
	ctx, cancel := context.WithTimeout(ctx, 150*time.Second)
	defer cancel()
	cfg, err := repo.Config(ctx, job)
	if err != nil {
		return err
	}
	var credential []byte
	if cfg.HasCredential {
		credential, err = embeddingCredential(key, cfg, cfg.EncryptedCredential, false)
		if err != nil {
			return err
		}
	}
	inputs, err := repo.Read(ctx, job)
	if err != nil {
		return err
	}
	texts := make([]string, len(inputs))
	for i, v := range inputs {
		texts[i] = v.Input
	}
	var vectors [][]float32
	if len(inputs) > 0 {
		vectors, err = client.Embed(ctx, cfg, string(credential), texts)
		if err != nil {
			return err
		}
	}
	if len(vectors) != len(inputs) {
		return errors.New("invalid embedding count")
	}
	outputs := make([]repositories.KnowledgeEmbeddingOutput, len(inputs))
	for i, v := range vectors {
		if len(v) != 1024 {
			return errors.New("invalid embedding dimensions")
		}
		var norm float64
		parts := make([]string, len(v))
		for k, x := range v {
			if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
				return errors.New("nonfinite embedding")
			}
			norm += float64(x) * float64(x)
			parts[k] = strconv.FormatFloat(float64(x), 'g', -1, 32)
		}
		if math.Abs(math.Sqrt(norm)-1) > 0.001 {
			return errors.New("unnormalized embedding")
		}
		outputs[i] = repositories.KnowledgeEmbeddingOutput{ChunkID: inputs[i].ChunkID, ContentHash: inputs[i].ContentHash, Embedding: "[" + strings.Join(parts, ",") + "]"}
	}
	ok, err := repo.Commit(ctx, job, outputs)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("stale embedding lease")
	}
	return nil
}
