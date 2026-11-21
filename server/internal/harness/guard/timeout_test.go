package guard_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"hsg/internal/harness/guard"
)

func TestTimeoutStopsWorkAfterDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := guard.RunWithTimeout(ctx, time.Millisecond, func(context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v; want DeadlineExceeded", err)
	}
}
