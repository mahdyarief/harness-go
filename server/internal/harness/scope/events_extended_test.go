package scope_test

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hsg/internal/harness/scope"
)

func TestParallelRunsAllListenersAndJoinsErrors(t *testing.T) {
	root := scope.New(nil)
	var mu sync.Mutex
	var slowDone, fastDone bool
	scope.On(root, "fanout", func(ev *scope.Event[policyEvent]) error {
		time.Sleep(5 * time.Millisecond)
		mu.Lock()
		slowDone = true
		mu.Unlock()
		return nil
	}, scope.EventOptions{Prepend: true})
	scope.On(root, "fanout", func(ev *scope.Event[policyEvent]) error {
		mu.Lock()
		fastDone = true
		mu.Unlock()
		return nil
	})

	start := time.Now()
	err := scope.Parallel(root, scope.Event[policyEvent]{Name: "fanout"})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatal(err)
	}
	if !slowDone || !fastDone {
		t.Fatalf("done: slow=%v fast=%v; want both", slowDone, fastDone)
	}
	if elapsed >= 10*time.Millisecond {
		t.Fatalf("elapsed = %v; listeners ran serially", elapsed)
	}
}

func TestPrependOrdersSerialListeners(t *testing.T) {
	root := scope.New(nil)
	var order []string
	scope.On(root, "pipeline", func(ev *scope.Event[policyEvent]) error {
		order = append(order, "first")
		return nil
	})
	scope.On(root, "pipeline", func(ev *scope.Event[policyEvent]) error {
		order = append(order, "second")
		return nil
	}, scope.EventOptions{Prepend: true})

	if err := scope.Serial(root, scope.Event[policyEvent]{Name: "pipeline"}); err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 || order[0] != "second" || order[1] != "first" {
		t.Fatalf("order = %v; want [second first]", order)
	}
}

func TestParallelJoinsErrors(t *testing.T) {
	root := scope.New(nil)
	first := errors.New("first")
	second := errors.New("second")
	scope.On(root, "fanout", func(ev *scope.Event[policyEvent]) error { return first })
	scope.On(root, "fanout", func(ev *scope.Event[policyEvent]) error { return second })

	err := scope.Parallel(root, scope.Event[policyEvent]{Name: "fanout"})
	if err == nil || !strings.Contains(err.Error(), "first") || !strings.Contains(err.Error(), "second") {
		t.Fatalf("err = %v; want both errors", err)
	}
}

func TestSerialStopsAtFirstBail(t *testing.T) {
	root := scope.New(nil)
	var calls []string
	scope.On(root, "pipeline", func(ev *scope.Event[policyEvent]) error {
		calls = append(calls, "first")
		return nil
	})
	scope.On(root, "pipeline", func(ev *scope.Event[policyEvent]) error {
		calls = append(calls, "second")
		return scope.ErrBail
	}, scope.EventOptions{Prepend: true})

	err := scope.Serial(root, scope.Event[policyEvent]{Name: "pipeline"})
	if !errors.Is(err, scope.ErrBail) {
		t.Fatalf("err = %v; want ErrBail", err)
	}
	if len(calls) != 1 || calls[0] != "second" {
		t.Fatalf("calls = %v; want only second", calls)
	}
}

func TestOnceDisposesAfterFirstCall(t *testing.T) {
	root := scope.New(nil)
	var count int
	scope.Once(root, "startup", func(ev *scope.Event[policyEvent]) error {
		count++
		return nil
	})

	if err := scope.Emit(root, scope.Event[policyEvent]{Name: "startup"}); err != nil {
		t.Fatal(err)
	}
	if err := scope.Emit(root, scope.Event[policyEvent]{Name: "startup"}); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("count = %d; want 1", count)
	}
}

func TestOnDisposerRemovesListener(t *testing.T) {
	root := scope.New(nil)
	var count int
	dispose := scope.On(root, "tick", func(ev *scope.Event[policyEvent]) error {
		count++
		return nil
	})
	if err := scope.Emit(root, scope.Event[policyEvent]{Name: "tick"}); err != nil {
		t.Fatal(err)
	}
	if err := dispose(); err != nil {
		t.Fatal(err)
	}
	if err := scope.Emit(root, scope.Event[policyEvent]{Name: "tick"}); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("count = %d; want 1", count)
	}
	if dispose() == nil {
		t.Fatal("second dispose should report already removed")
	}
}

func TestBailStopsOnFirstBailSignal(t *testing.T) {
	root := scope.New(nil)
	var count atomic.Int32
	scope.On(root, "check", func(ev *scope.Event[policyEvent]) error {
		count.Add(1)
		return nil
	})
	scope.On(root, "check", func(ev *scope.Event[policyEvent]) error {
		count.Add(1)
		return scope.ErrBail
	}, scope.EventOptions{Prepend: true})

	stopped, err := scope.Bail(root, scope.Event[policyEvent]{Name: "check"})
	if !stopped || !errors.Is(err, scope.ErrBail) {
		t.Fatalf("bail = %v, %v; want true, ErrBail", stopped, err)
	}
	if count.Load() != 1 {
		t.Fatalf("count = %d; want 1", count.Load())
	}
}

func TestWaterfallNextCanVeto(t *testing.T) {
	root := scope.New(nil)
	var reached bool
	scope.OnNext(root, "build", func(next func() error) func() error {
		return func() error {
			reached = true
			return nil
		}
	})
	scope.OnNext(root, "build", func(next func() error) func() error {
		return func() error { return nil }
	}, scope.EventOptions{Prepend: true})

	err := scope.WaterfallNext(root, "build", func() error {
		t.Fatal("base handler must not run when vetoed")
		return nil
	})
	if err != nil || reached {
		t.Fatalf("veto: err=%v reached=%v", err, reached)
	}
}

func TestWaterfallNextDelegatesWhenListenerCallsNext(t *testing.T) {
	root := scope.New(nil)
	var baseRan bool
	scope.OnNext(root, "build", func(next func() error) func() error {
		return func() error { return next() }
	})

	err := scope.WaterfallNext(root, "build", func() error {
		baseRan = true
		return nil
	})
	if err != nil || !baseRan {
		t.Fatalf("delegate: err=%v baseRan=%v", err, baseRan)
	}
}
