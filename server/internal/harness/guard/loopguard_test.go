package guard

import (
	"testing"

	"hsg/internal/harness/services"
)

func failed(name, args, err string) services.ToolRecord {
	return services.ToolRecord{Name: name, Arguments: args, Err: err}
}

func TestRepeatedErrorSignature(t *testing.T) {
	if errorSignature(services.ToolRecord{Name: "a", Arguments: `{"q":"x"}`}) != "" {
		t.Fatal("a successful call must have no signature")
	}
	a := errorSignature(failed("a", `{"q": "x", "n": 1}`, "tool execution failed"))
	b := errorSignature(failed("a", `{"n":1,"q":"x"}`, "tool execution failed"))
	if a == "" || a != b {
		t.Fatalf("key order and whitespace must not change the signature: %q vs %q", a, b)
	}
	for name, other := range map[string]services.ToolRecord{
		"tool":      failed("b", `{"q":"x","n":1}`, "tool execution failed"),
		"arguments": failed("a", `{"q":"y","n":1}`, "tool execution failed"),
		"error":     failed("a", `{"q":"x","n":1}`, "tool timed out"),
	} {
		if errorSignature(other) == a {
			t.Fatalf("a different %s must produce a different signature", name)
		}
	}
	m1 := errorSignature(failed("a", "{not json", "e"))
	m2 := errorSignature(failed("a", "{not   json", "e"))
	if m1 == "" || m1 != m2 {
		t.Fatal("malformed arguments must still match themselves")
	}
}

func TestRepeatedErrorGuardInspect(t *testing.T) {
	g := NewRepeatedErrorGuard()
	mem := services.GuardMemory{}
	same := services.GuardTurn{Records: []services.ToolRecord{failed("a", `{"q":"x"}`, "tool execution failed")}}
	if v := g.Inspect(mem, same); !v.Allowed() {
		t.Fatal("the first failure must give the model a chance to recover")
	}
	if v := g.Inspect(mem, services.GuardTurn{Records: []services.ToolRecord{{Name: "a", Arguments: `{"q":"x"}`}}}); !v.Allowed() {
		t.Fatal("a success must not count")
	}
	v := g.Inspect(mem, same)
	if v.Allowed() || v.Reason != repeatedErrorAnswer {
		t.Fatalf("the second identical failure must stop the run: %+v", v)
	}
}
