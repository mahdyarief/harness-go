package llm

import (
	"context"
	"math/rand"
	"time"
)

// RetryPolicy mirrors DSH's exponential-backoff policy. The agent loop keeps
// LoopConfig.MaxRetries (bounded 0-3) as its cap instead of DSH's fixed 5.
type RetryPolicy struct {
	MaxRetries int
	Initial    time.Duration
	Max        time.Duration
	Jitter     float64
	Codes      map[string]bool
	Sleep      func(context.Context, time.Duration) error
	Rand       func() float64
}

func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxRetries: 3,
		Initial:    500 * time.Millisecond,
		Max:        10 * time.Second,
		Jitter:     0.1,
		Codes:      DefaultRetryableCodes,
		Sleep:      sleepWithContext,
		Rand:       rand.Float64,
	}
}

func (p RetryPolicy) Retryable(err error) bool {
	code := FailureCodeOf(err)
	return code != "" && p.Codes[code]
}

// Delay returns attempt N's backoff: Initial * 2^(N-1), capped at Max, with
// symmetric jitter applied to the capped value.
func (p RetryPolicy) Delay(attempt int) time.Duration {
	initial := p.Initial
	if initial <= 0 {
		initial = 500 * time.Millisecond
	}
	max := p.Max
	if max <= 0 {
		max = 10 * time.Second
	}
	d := initial
	for i := 1; i < attempt && d < max; i++ {
		d *= 2
	}
	if d > max {
		d = max
	}
	if p.Jitter > 0 {
		rng := p.Rand
		if rng == nil {
			rng = rand.Float64
		}
		// Center jitter around 1: ±Jitter fraction.
		factor := 1 + (rng()*2-1)*p.Jitter
		d = time.Duration(float64(d) * factor)
	}
	return d
}

// Do runs op and retries only on the policy's stable codes. A caller can reset
// the retry budget simply by calling Do again for the next step.
func (p RetryPolicy) Do(ctx context.Context, op func() (int, error)) (int, error) {
	sleep := p.Sleep
	if sleep == nil {
		sleep = sleepWithContext
	}
	var result int
	var err error
	for attempt := 0; ; attempt++ {
		result, err = op()
		if err == nil || !p.Retryable(err) || attempt >= p.MaxRetries {
			return result, err
		}
		if e := sleep(ctx, p.Delay(attempt+1)); e != nil {
			return result, e
		}
	}
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
