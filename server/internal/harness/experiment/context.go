package experiment

import (
	"context"

	"hsg/internal/harness/services"
)

// The runner hands each call its run id and optional sink through the context,
// so an arm that writes a run log tags every event without changing Run's
// signature.
type contextKey int

const (
	runIDKey contextKey = iota
	sinkKey
)

// WithRunID tags ctx with the run id assigned to this arm×item.
func WithRunID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, runIDKey, id)
}

// RunIDFrom returns the assigned run id, or "" outside a runner call.
func RunIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(runIDKey).(string)
	return id
}

// WithSink attaches the run-log sink for this arm×item.
func WithSink(ctx context.Context, sink services.Sink) context.Context {
	return context.WithValue(ctx, sinkKey, sink)
}

// SinkFrom returns the attached sink, or nil.
func SinkFrom(ctx context.Context) services.Sink {
	sink, _ := ctx.Value(sinkKey).(services.Sink)
	return sink
}
