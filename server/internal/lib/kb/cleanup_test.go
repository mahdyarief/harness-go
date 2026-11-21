package kb

import (
	"context"
	"errors"
	"testing"
	"time"
	"hsg/internal/lib/blob"
)

type cleanupStub struct {
	keys                               []string
	pendingErr, completeErr, deleteErr error
	completed, deleted                 []string
	hook                               func(context.Context)
}

func (s *cleanupStub) Pending(context.Context, int) ([]string, error) { return s.keys, s.pendingErr }
func (s *cleanupStub) Complete(_ context.Context, key string) error {
	s.completed = append(s.completed, key)
	return s.completeErr
}
func (s *cleanupStub) Delete(ctx context.Context, key string) error {
	s.deleted = append(s.deleted, key)
	if _, ok := ctx.Deadline(); !ok {
		panic("missing operation deadline")
	}
	if s.hook != nil {
		s.hook(ctx)
	}
	return s.deleteErr
}
func TestCleanupOnce(t *testing.T) {
	failure := errors.New("backend unavailable")
	for _, tc := range []struct {
		name         string
		storage, ack error
		completed    int
	}{
		{"success", nil, nil, 1}, {"missing_is_success", blob.ErrNotFound, nil, 1},
		{"storage_failure_retains_job", failure, nil, 0}, {"ack_failure_retries", nil, failure, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &cleanupStub{keys: []string{"opaque"}, deleteErr: tc.storage, completeErr: tc.ack}
			err := CleanupOnce(context.Background(), s, s)
			wantErr := tc.storage != nil && !errors.Is(tc.storage, blob.ErrNotFound) || tc.ack != nil
			if (err != nil) != wantErr || len(s.completed) != tc.completed {
				t.Fatalf("err=%v completed=%v", err, s.completed)
			}
		})
	}
	s := &cleanupStub{pendingErr: failure}
	if !errors.Is(CleanupOnce(context.Background(), s, s), failure) || len(s.deleted) != 0 {
		t.Fatal("queue failure ignored")
	}
}

func TestCleanupRunnerStartupPeriodicCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &cleanupStub{keys: []string{"opaque"}}
	s.hook = func(context.Context) {
		if len(s.deleted) == 2 {
			cancel()
		}
	}
	done := make(chan struct{})
	go func() { defer close(done); RunCleanup(ctx, s, s, time.Millisecond, nil) }()
	select {
	case <-done:
		if len(s.deleted) != 2 {
			t.Fatalf("startup/periodic deletes=%d", len(s.deleted))
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not cancel")
	}
	stopped, stop := context.WithCancel(context.Background())
	stop()
	s = &cleanupStub{keys: []string{"opaque"}}
	if !errors.Is(CleanupOnce(stopped, s, s), context.Canceled) || len(s.deleted) != 0 {
		t.Fatal("deleted after cancellation")
	}
}
