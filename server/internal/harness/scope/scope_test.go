package scope_test

import (
	"errors"
	"strings"
	"testing"

	"hsg/internal/harness/scope"
)

var answerKey = scope.NewKey[int]("answer")

func TestProvideRequireAndParentFallback(t *testing.T) {
	root := scope.New(nil)
	if err := root.Provide(answerKey, 42, nil); err != nil {
		t.Fatal(err)
	}
	child := root.Child()
	got, ok := scope.Require(child, answerKey)
	if !ok || got != 42 {
		t.Fatalf("got %v, %v; want 42, true", got, ok)
	}
}

func TestDisposeRunsLIFOAndJoinsErrors(t *testing.T) {
	order := []string{}
	root := scope.New(nil)
	a := scope.NewKey[string]("a")
	b := scope.NewKey[string]("b")
	_ = root.Provide(a, "a", func() error { order = append(order, "a"); return nil })
	_ = root.Provide(b, "b", func() error { order = append(order, "b"); return errors.New("boom") })
	err := root.Dispose()
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("want joined boom error, got %v", err)
	}
	if len(order) != 2 || order[0] != "b" || order[1] != "a" {
		t.Fatalf("dispose order = %v; want [b a]", order)
	}
}
