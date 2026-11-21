package scope_test

import (
	"errors"
	"testing"

	"hsg/internal/harness/scope"
)

type policyEvent struct{ Value int }

func TestEmitWaterfallAndBail(t *testing.T) {
	root := scope.New(nil)
	var seen int
	scope.On(root, "chat.delta", func(ev *scope.Event[policyEvent]) error {
		seen = ev.Payload.Value
		return nil
	})
	if err := scope.Emit(root, scope.Event[policyEvent]{Name: "chat.delta", Payload: policyEvent{Value: 7}}); err != nil {
		t.Fatal(err)
	}
	if seen != 7 {
		t.Fatalf("seen = %d; want 7", seen)
	}

	scope.On(root, "tool.before", func(ev *scope.Event[policyEvent]) error { return nil })
	scope.On(root, "tool.before", func(ev *scope.Event[policyEvent]) error {
		ev.Payload.Value += 5
		return nil
	})
	got, err := scope.Waterfall(root, scope.Event[policyEvent]{Name: "tool.before"}, policyEvent{Value: 1})
	if err != nil || got.Value != 6 {
		t.Fatalf("waterfall = %v, %v; want 6, nil", got, err)
	}

	scope.On(root, "tool.guard", func(ev *scope.Event[policyEvent]) error { return errors.New("denied") })
	stopped, err := scope.Bail(root, scope.Event[policyEvent]{Name: "tool.guard"})
	if !stopped || err == nil {
		t.Fatalf("bail = %v, %v; want true, error", stopped, err)
	}
}
