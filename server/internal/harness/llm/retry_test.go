package llm

import (
	"context"
	"errors"
	"math/rand"
	"testing"
	"time"

	"hsg/internal/harness/services"
)

func TestRetryDelayExponentialWithCap(t *testing.T) {
	p := RetryPolicy{Initial: 500 * time.Millisecond, Max: 10 * time.Second, Jitter: 0}
	if got := p.Delay(1); got != 500*time.Millisecond {
		t.Fatalf("Delay(1)=%v; want 500ms", got)
	}
	if got := p.Delay(2); got != time.Second {
		t.Fatalf("Delay(2)=%v; want 1s", got)
	}
	if got := p.Delay(10); got != 10*time.Second {
		t.Fatalf("Delay(10)=%v; want capped 10s", got)
	}
}

func TestRetryDelayJitterWithinTenPercent(t *testing.T) {
	p := RetryPolicy{Initial: time.Second, Max: 10 * time.Second, Jitter: 0.1, Rand: rand.New(rand.NewSource(1)).Float64}
	for i := 0; i < 100; i++ {
		got := p.Delay(1)
		if got < 900*time.Millisecond || got > 1100*time.Millisecond {
			t.Fatalf("Delay(1)=%v outside ±10%%", got)
		}
	}
}

func TestRetryPolicyRoutesOnCode(t *testing.T) {
	p := DefaultRetryPolicy()
	if !p.Retryable(services.NewHarnessError("x", string(CodeRateLimit), nil)) {
		t.Fatal("RATE_LIMIT must retry")
	}
	for _, code := range []FailureCode{CodeAuth, CodeQuota, CodeContextWindow} {
		if p.Retryable(services.NewHarnessError("x", string(code), nil)) {
			t.Fatalf("%s must not retry", code)
		}
	}
	if p.Retryable(errors.New("plain")) {
		t.Fatal("an unclassified error must not retry")
	}
}

func TestRetryStopsOnSleepCancellation(t *testing.T) {
	p := RetryPolicy{
		MaxRetries: 3, Initial: time.Millisecond, Max: time.Millisecond, Jitter: 0,
		Codes: DefaultRetryableCodes,
		Sleep: func(context.Context, time.Duration) error { return context.Canceled },
	}
	var calls int
	_, err := p.Do(context.Background(), func() (int, error) {
		calls++
		return 0, services.NewHarnessError("x", string(CodeRateLimit), nil)
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v; want context.Canceled", err)
	}
	if calls != 1 {
		t.Fatalf("calls=%d; sleep cancellation must stop immediately", calls)
	}
}

func TestRetryCountResetsPerStep(t *testing.T) {
	p := RetryPolicy{
		MaxRetries: 2, Initial: time.Millisecond, Max: time.Millisecond, Jitter: 0,
		Codes: DefaultRetryableCodes,
		Sleep: func(context.Context, time.Duration) error { return nil },
	}
	var calls int
	value, err := p.Do(context.Background(), func() (int, error) {
		calls++
		if calls <= 2 {
			return 0, services.NewHarnessError("x", string(CodeRateLimit), nil)
		}
		return 7, nil
	})
	if err != nil || value != 7 || calls != 3 {
		t.Fatalf("value=%d calls=%d err=%v; want 7,3,nil", value, calls, err)
	}
}
