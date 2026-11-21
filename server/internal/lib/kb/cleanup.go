package kb

import (
	"context"
	"errors"
	"log/slog"
	"time"
	"hsg/internal/lib/blob"
)

// CleanupQueue persists opaque keys independently of request lifetimes.
type CleanupQueue interface {
	Pending(context.Context, int) ([]string, error)
	Complete(context.Context, string) error
}
type BlobDeleter interface {
	Delete(context.Context, string) error
}

// CleanupOnce bounds each batch and object operation. Jobs survive storage
// errors, cancellation and acknowledgement errors. Duplicate workers are safe
// because both storage deletion and queue completion are idempotent.
func CleanupOnce(ctx context.Context, queue CleanupQueue, store BlobDeleter) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	keys, err := queue.Pending(ctx, 100)
	if err != nil {
		return err
	}
	var failures []error
	for _, key := range keys {
		if ctx.Err() != nil {
			return errors.Join(append(failures, ctx.Err())...)
		}
		jobCtx, stop := context.WithTimeout(ctx, 5*time.Second)
		err := store.Delete(jobCtx, key)
		if err == nil || errors.Is(err, blob.ErrNotFound) {
			err = queue.Complete(jobCtx, key)
		}
		stop()
		if err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// RunCleanup sweeps at startup and periodically until canceled. Log a fixed
// message only: backend errors might contain sensitive information.
func RunCleanup(ctx context.Context, queue CleanupQueue, store BlobDeleter, interval time.Duration, logger *slog.Logger) {
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		if err := CleanupOnce(ctx, queue, store); err != nil && ctx.Err() == nil && logger != nil {
			logger.Warn("knowledge blob cleanup incomplete; pending jobs will be retried")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
