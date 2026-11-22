# Harness-Go Rewrite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rewrite the HSG Go agent harness with Cordis-style service boundaries and DeepSeek-Harness-inspired guardrails, keeping SSE and billing behavior stable.

**Architecture:** Create `server/internal/harness/` with a typed service `Scope`, event dispatch, swappable `AgentLoop`, and a guarded tool pipeline. `lib/agent` becomes the default loop implementation behind new interfaces; `lib/ai` remains the provider transport.

**Tech Stack:** Go 1.27.3, module `hsg`, Fiber v3 handler, standard-library `testing`, existing `internal/lib/ai` and repositories.

## Global Constraints

- Module path is `hsg`; no wording `validai` in new code or docs.
- No React UI, Authula, tenant RLS, database schema, or migration changes.
- SSE `Event` JSON tags and billing calls keep their current wire shape.
- New packages under `server/internal/harness/` import neither Fiber nor `database/sql`.
- Configuration is read only in `cmd/api/foundation_config.go`.
- Sandbox default is `read-only`; `workspace-write` is explicit opt-in.
- Every task ends with focused Go tests passing and a commit.

---

## Execution status

**Status: DONE** in `a128621` (feat: hsg harness-go foundation) and `d8200c0` (align harness with deepseek harness guardrails). The boxes were ticked after the fact, during the 2026-11-22 plan review.

Deviations from the text below:
- The code samples had compile bugs (`s.On` as a generic method, a waterfall test mutating a by-value event). The implementation fixed them.
- Task 3 `Sessions` / `SessionsKey`: the interface was declared but never implemented. It is replaced by `RunLog` in `2026-11-22-harness-foundation-capabilities.md` Task 5.
- Task 3 `GuardsKey`: removed in `d983a97`. Loop guards are passed through `MountConfig.LoopGuards`.
- Task 8 behaviour gaps (budgets, concurrency, resume, retry, pipeline) were closed by `2026-11-21-dsh-core-parity.md`.

### Task 1: Typed service scope with disposers

**Files:**
- Create: `server/internal/harness/scope/scope.go`
- Create: `server/internal/harness/scope/scope_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `type Key[T any] struct{ name string }`, `func NewKey[T any](name string) Key[T]`, `type Disposer func() error`, `type Scope struct`, `func New(parent *Scope) *Scope`, `func (s *Scope) Provide(key fmt.Stringer, value any, dispose Disposer) error`, `func Require[T any](s *Scope, key Key[T]) (T, bool)`, `func (s *Scope) Child() *Scope`, `func (s *Scope) Dispose() error`.

- [x] **Step 1: Write failing test**

```go
package scope_test

import (
	"errors"
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
```

Add `strings` to the test imports.

- [x] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/harness/scope -v`
Expected: FAIL with "undefined: scope"

- [x] **Step 3: Implement minimal scope**

```go
package scope

import (
	"errors"
	"fmt"
)

type Key[T any] struct{ name string }

func NewKey[T any](name string) Key[T] { return Key[T]{name: name} }
func (k Key[T]) String() string        { return k.name }

type Disposer func() error

type service struct {
	value   any
	dispose Disposer
}

type Scope struct {
	parent   *Scope
	services map[string]service
	order    []string
}

func New(parent *Scope) *Scope {
	return &Scope{parent: parent, services: map[string]service{}}
}

func (s *Scope) Provide(key fmt.Stringer, value any, dispose Disposer) error {
	name := key.String()
	if _, exists := s.services[name]; exists {
		return fmt.Errorf("service %q already provided in scope", name)
	}
	s.services[name] = service{value: value, dispose: dispose}
	s.order = append(s.order, name)
	return nil
}

func Require[T any](s *Scope, key Key[T]) (T, bool) {
	var zero T
	for cur := s; cur != nil; cur = cur.parent {
		svc, ok := cur.services[key.String()]
		if !ok {
			continue
		}
		v, ok := svc.value.(T)
		return v, ok
	}
	return zero, false
}

func (s *Scope) Child() *Scope { return New(s) }

func (s *Scope) Dispose() error {
	var errs []error
	for i := len(s.order) - 1; i >= 0; i-- {
		svc := s.services[s.order[i]]
		if svc.dispose != nil {
			if err := svc.dispose(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	s.services = map[string]service{}
	s.order = nil
	return errors.Join(errs...)
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `cd server && go test ./internal/harness/scope -v`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add server/internal/harness/scope
git commit -m "feat(harness): add typed service scope with disposers"
```

---

### Task 2: Typed event dispatch

**Files:**
- Create: `server/internal/harness/scope/events.go`
- Create: `server/internal/harness/scope/events_test.go`

**Interfaces:**
- Consumes: `Scope` from Task 1.
- Produces: `type Event[T any] struct{ Name string; Payload T }`, `func (s *Scope) On[T any](name string, fn func(Event[T]) error) Disposer`, `func Emit[T any](s *Scope, ev Event[T]) error`, `func Waterfall[T any](s *Scope, ev Event[T], seed T) (T, error)`, `func Bail[T any](s *Scope, ev Event[T]) (bool, error)`.

- [x] **Step 1: Write failing test**

```go
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
	root.On("chat.delta", func(ev scope.Event[policyEvent]) error {
		seen = ev.Payload.Value
		return nil
	})
	if err := scope.Emit(root, scope.Event[policyEvent]{Name: "chat.delta", Payload: policyEvent{Value: 7}}); err != nil {
		t.Fatal(err)
	}
	if seen != 7 {
		t.Fatalf("seen = %d; want 7", seen)
	}

	root.On("tool.before", func(ev scope.Event[policyEvent]) error { return nil })
	root.On("tool.before", func(ev scope.Event[policyEvent]) error {
		ev.Payload.Value += 5
		return nil
	})
	got, err := scope.Waterfall(root, scope.Event[policyEvent]{Name: "tool.before"}, policyEvent{Value: 1})
	if err != nil || got.Value != 6 {
		t.Fatalf("waterfall = %v, %v; want 6, nil", got, err)
	}

	root.On("tool.guard", func(ev scope.Event[policyEvent]) error { return errors.New("denied") })
	stopped, err := scope.Bail(root, scope.Event[policyEvent]{Name: "tool.guard"})
	if !stopped || err == nil {
		t.Fatalf("bail = %v, %v; want true, error", stopped, err)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/harness/scope -run TestEmitWaterfallAndBail -v`
Expected: FAIL with "undefined: scope.Event"

- [x] **Step 3: Implement event dispatch**

```go
package scope

type Event[T any] struct {
	Name    string
	Payload T
}

type listener[T any] func(Event[T]) error

type listenerBox[T any] struct{ fns []listener[T] }

func eventBoxKey[T any](name string) Key[*listenerBox[T]] {
	return NewKey[*listenerBox[T]]("event:" + name)
}

func On[T any](s *Scope, name string, fn listener[T]) Disposer {
	key := eventBoxKey[T](name)
	box, ok := Require(s, key)
	if !ok {
		box = &listenerBox[T]{}
		_ = s.Provide(key, box, nil)
	}
	box.fns = append(box.fns, fn)
	return func() error { return nil }
}

func listeners[T any](s *Scope, name string) []listener[T] {
	box, ok := Require(s, eventBoxKey[T](name))
	if !ok {
		return nil
	}
	return box.fns
}

func Emit[T any](s *Scope, ev Event[T]) error {
	for _, fn := range listeners[T](s, ev.Name) {
		if err := fn(ev); err != nil {
			return err
		}
	}
	return nil
}

func Waterfall[T any](s *Scope, ev Event[T], seed T) (T, error) {
	out := seed
	for _, fn := range listeners[T](s, ev.Name) {
		ev.Payload = out
		if err := fn(ev); err != nil {
			return out, err
		}
		out = ev.Payload
	}
	return out, nil
}

func Bail[T any](s *Scope, ev Event[T]) (bool, error) {
	for _, fn := range listeners[T](s, ev.Name) {
		if err := fn(ev); err != nil {
			return true, err
		}
	}
	return false, nil
}
```

Split this into separate files (`scope/events.go` and `scope/events_test.go`) and add any missing imports with `goimports`-style grouping.

- [x] **Step 4: Run test to verify it passes**

Run: `cd server && go test ./internal/harness/scope -v`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add server/internal/harness/scope
git commit -m "feat(harness): add emit, waterfall, and bail events"
```

---

### Task 3: Service interfaces for agent, loop, tools, LLM, sessions, guards

**Files:**
- Create: `server/internal/harness/services/services.go`
- Create: `server/internal/harness/services/services_test.go`

**Interfaces:**
- Consumes: `scope.Key` from Task 1.
- Produces: keys and interfaces used by all later tasks: `AgentKey`, `AgentLoopKey`, `ToolsKey`, `LLMKey`, `SessionsKey`, `SystemPromptKey`, `GuardsKey`, `SandboxKey`, `ApprovalKey`, `Agent`, `AgentLoop`, `Tool`, `ToolResult`, `Reference`, `LLM`, `Sessions`, `GuardDecision`.

- [x] **Step 1: Write failing test**

```go
package services_test

import (
	"context"
	"testing"

	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
)

type fakeAgent struct{}

func (fakeAgent) Run(context.Context, services.RunRequest, services.Sink) (services.Outcome, error) {
	return services.Outcome{}, nil
}

func TestAgentKeyResolvesAgent(t *testing.T) {
	s := scope.New(nil)
	if err := s.Provide(services.AgentKey, fakeAgent{}, nil); err != nil {
		t.Fatal(err)
	}
	agent, ok := scope.Require(s, services.AgentKey)
	if !ok || agent == nil {
		t.Fatal("AgentKey did not resolve")
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/harness/services -v`
Expected: FAIL with "undefined: services.AgentKey"

- [x] **Step 3: Implement service contracts**

```go
package services

import (
	"context"
	"encoding/json"

	"hsg/internal/harness/scope"
	"hsg/internal/lib/ai"
)

type RunRequest struct {
	Messages   []ai.Message
	ToolNames  []string
	SystemText string
}

type Sink func(AIEvent) error

type AIEvent struct {
	Type       string `json:"type"`
	Text       string `json:"text,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	ToolCall   string `json:"tool_call,omitempty"`
	Error      string `json:"error,omitempty"`
	DurationMs int    `json:"duration_ms,omitempty"`
}

type Outcome struct {
	Content          string
	PromptTokens     int
	CompletionTokens int
	Turns            int
	ToolCalls        int
	References       []Reference
	DurationMs       int
	Partial          bool
}

type Agent interface {
	Run(ctx context.Context, req RunRequest, sink Sink) (Outcome, error)
}

type AgentLoop interface {
	Run(ctx context.Context, req RunRequest, sink Sink) (Outcome, error)
}

type Reference struct {
	DocumentID  string `json:"document_id"`
	ChunkID     string `json:"chunk_id"`
	Title       string `json:"title"`
	Filename    string `json:"filename"`
	Format      string `json:"format"`
	Locator     string `json:"locator"`
	HeadingPath string `json:"heading_path"`
	Position    int    `json:"position"`
	Excerpt     string `json:"excerpt"`
	Source      string `json:"source,omitempty"`
}

type ToolResult struct {
	Output     string
	References []Reference
}

type Tool interface {
	Name() string
	Description() string
	Parameters() json.RawMessage
	ReadOnly() bool
	Execute(ctx context.Context, args json.RawMessage) (ToolResult, error)
}

type Tools interface {
	Lookup(name string) (Tool, bool)
	Specs() []ai.ToolSpec
}

type LLM interface {
	MaxTokens() int
	ChatStream(ctx context.Context, messages []ai.Message, opts ai.ChatOptions, onDelta func(ai.StreamDelta) error) (ai.ChatResult, error)
}

type Sessions interface {
	Append(turn Outcome) error
}

type GuardDecision struct {
	Allowed bool
	Reason  string
}

var (
	AgentKey        = scope.NewKey[Agent]("agent")
	AgentLoopKey    = scope.NewKey[AgentLoop]("agentLoop")
	ToolsKey        = scope.NewKey[Tools]("tools")
	LLMKey          = scope.NewKey[LLM]("llm")
	SessionsKey     = scope.NewKey[Sessions]("sessions")
	SystemPromptKey = scope.NewKey[string]("systemPrompt")
	GuardsKey       = scope.NewKey[[]GuardDecision]("guards")
	SandboxKey      = scope.NewKey[string]("sandboxPolicy")
	ApprovalKey     = scope.NewKey[string]("approvalPolicy")
)
```

Remove the self-referential alias lines if the compiler reports a cycle; the goal is to keep `Reference` wire-compatible with the old `agent.Reference` JSON shape.

- [x] **Step 4: Run test to verify it passes**

Run: `cd server && go test ./internal/harness/services -v`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add server/internal/harness/services
git commit -m "feat(harness): define service keys and contracts"
```

---

### Task 4: Agent facade resolves AgentLoop from scope

**Files:**
- Create: `server/internal/harness/core/agent.go`
- Create: `server/internal/harness/core/agent_test.go`

**Interfaces:**
- Consumes: `services.Agent`, `services.AgentLoop`, `scope.Require` from Tasks 1-3.
- Produces: `func NewAgent(s *scope.Scope) services.Agent`.

- [x] **Step 1: Write failing test**

```go
package core_test

import (
	"context"
	"testing"

	"hsg/internal/harness/core"
	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
)

type stubLoop struct{ called bool }

func (s *stubLoop) Run(context.Context, services.RunRequest, services.Sink) (services.Outcome, error) {
	s.called = true
	return services.Outcome{Content: "ok"}, nil
}

func TestFacadeDelegatesToLoop(t *testing.T) {
	s := scope.New(nil)
	loop := &stubLoop{}
	_ = s.Provide(services.AgentLoopKey, services.AgentLoop(loop), nil)
	a := core.NewAgent(s)
	out, err := a.Run(context.Background(), services.RunRequest{}, nil)
	if err != nil || out.Content != "ok" || !loop.called {
		t.Fatalf("out=%+v err=%v called=%v", out, err, loop.called)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/harness/core -v`
Expected: FAIL with "undefined: core.NewAgent"

- [x] **Step 3: Implement facade**

```go
package core

import (
	"context"

	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
)

type agentFacade struct{ scope *scope.Scope }

func NewAgent(s *scope.Scope) services.Agent { return &agentFacade{scope: s} }

func (a *agentFacade) Run(ctx context.Context, req services.RunRequest, sink services.Sink) (services.Outcome, error) {
	loop, ok := scope.Require(a.scope, services.AgentLoopKey)
	if !ok {
		return services.Outcome{}, services.ErrLoopMissing
	}
	return loop.Run(ctx, req, sink)
}
```

Add `var ErrLoopMissing = errors.New("agentLoop service missing")` to `services/services.go`.

- [x] **Step 4: Run test to verify it passes**

Run: `cd server && go test ./internal/harness/core -v`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add server/internal/harness/core server/internal/harness/services
git commit -m "feat(harness): add agent facade with swappable loop"
```

---

### Task 5: LLM adapter over existing gateway

**Files:**
- Create: `server/internal/harness/llm/adapter.go`
- Create: `server/internal/harness/llm/adapter_test.go`

**Interfaces:**
- Consumes: `services.LLM`, `ai.Gateway` from `lib/ai`.
- Produces: `func New(gw *ai.Gateway) services.LLM`.

- [x] **Step 1: Write failing test**

```go
package llm_test

import (
	"testing"

	"hsg/internal/harness/llm"
	"hsg/internal/lib/ai"
)

func TestAdapterReturnsConfigErrorForNilGateway(t *testing.T) {
	adapter := llm.New(nil)
	if adapter == nil {
		t.Fatal("adapter must not be nil")
	}
	_, err := adapter.ChatStream(nil, nil, ai.ChatOptions{}, nil)
	if err == nil {
		t.Fatal("want error from nil gateway")
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/harness/llm -v`
Expected: FAIL with "undefined: llm.New"

- [x] **Step 3: Implement adapter**

```go
package llm

import (
	"context"

	"hsg/internal/harness/services"
	"hsg/internal/lib/ai"
)

type adapter struct{ gw chatStreamer }

type chatStreamer interface {
	MaxTokens() int
	ChatStream(ctx context.Context, messages []ai.Message, opts ai.ChatOptions, onDelta func(ai.StreamDelta) error) (ai.ChatResult, error)
}

func New(gw *ai.Gateway) services.LLM {
	if gw == nil {
		return &adapter{}
	}
	return &adapter{gw: gw}
}

func (a *adapter) MaxTokens() int {
	if a == nil || a.gw == nil {
		return 0
	}
	return a.gw.MaxTokens()
}

func (a *adapter) ChatStream(ctx context.Context, messages []ai.Message, opts ai.ChatOptions, onDelta func(ai.StreamDelta) error) (ai.ChatResult, error) {
	if a == nil || a.gw == nil {
		return ai.ChatResult{}, ai.ErrConfig
	}
	return a.gw.ChatStream(ctx, messages, opts, onDelta)
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `cd server && go test ./internal/harness/llm -v`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add server/internal/harness/llm
git commit -m "feat(harness): add LLM adapter over ai gateway"
```

---

### Task 6: Guardrails: repeat tool, timeout, sandbox, approval, preset

**Files:**
- Create: `server/internal/harness/guard/repeat.go`
- Create: `server/internal/harness/guard/timeout.go`
- Create: `server/internal/harness/sandbox/policy.go`
- Create: `server/internal/harness/approval/policy.go`
- Create: `server/internal/harness/preset/preset.go`
- Create matching `_test.go` files for each.

**Interfaces:**
- Consumes: `services.GuardDecision` and scope keys from Task 3.
- Produces: `guard.NewRepeatTool(window int)`, `guard.NewTimeout(d time.Duration)`, `sandbox.Validate(cfg sandbox.Config) error`, `approval.Decide(cfg approval.Config, tool string) services.GuardDecision`, `preset.Apply(s *scope.Scope, name string) error`.

- [x] **Step 1: Write failing tests**

```go
// guard/repeat_test.go
package guard_test

import (
	"testing"

	"hsg/internal/harness/guard"
)

func TestRepeatToolFlagsExactRepeat(t *testing.T) {
	g := guard.NewRepeatTool(1)
	if !g("web_search", `{"q":"a"}`).Flagged {
		t.Fatal("first repeat must be flagged at window 1")
	}
}
```

Write analogous tests: timeout returns a `services.GuardDecision` with `Allowed=false` after the deadline; `sandbox.Validate` accepts `read-only` and rejects a relative `workspaceRoot` in `workspace-write`; `approval.Decide("never")` allows and `("ask")` returns `NeedsApproval`; `preset.Apply` installs allow-list, sandbox, and approval keys in a child scope.

- [x] **Step 2: Run tests to verify they fail**

Run: `cd server && go test ./internal/harness/guard ./internal/harness/sandbox ./internal/harness/approval ./internal/harness/preset -v`
Expected: FAIL with "undefined" for each package.

- [x] **Step 3: Implement guardrails**

Implement each package with no external dependencies. Repeat-tool uses a bounded map keyed by tool name plus canonicalized args JSON. Timeout wraps a `context.WithTimeout` call around tool execution helpers. Sandbox config carries `Mode`, `WorkspaceRoot`, `AdditionalWritableRoots`; `Validate` requires absolute paths and rejects trailing separators. Approval config carries `Policy` (`ask`/`never`) and returns a decision with `Reason` in Indonesian. Preset config is a map of named combinations of sandbox mode, approval policy, and tool allow-list; `Apply` provides those into the scope via `Provide`.

- [x] **Step 4: Run tests to verify they pass**

Run: `cd server && go test ./internal/harness/guard ./internal/harness/sandbox ./internal/harness/approval ./internal/harness/preset -v`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add server/internal/harness/guard server/internal/harness/sandbox server/internal/harness/approval server/internal/harness/preset
git commit -m "feat(harness): add DSH-inspired guardrails"
```

---

### Task 7: Guarded tool pipeline and adapter from old registry

**Files:**
- Create: `server/internal/harness/tools/pipeline.go`
- Create: `server/internal/harness/tools/adapter.go`
- Create: `server/internal/harness/tools/pipeline_test.go`

**Interfaces:**
- Consumes: `services.Tools`, `services.Tool`, `lib/agent.Registry` and `lib/agent.Tool`.
- Produces: `func Adapt(r *agent.Registry) services.Tools`, `func Pipeline(s *scope.Scope, next services.Tool) services.Tool`.

- [x] **Step 1: Write failing test**

```go
package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
	"hsg/internal/harness/tools"
)

type denyTool struct{}

func (denyTool) Name() string                { return "x" }
func (denyTool) Description() string         { return "x" }
func (denyTool) Parameters() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (denyTool) ReadOnly() bool              { return true }
func (denyTool) Execute(context.Context, json.RawMessage) (services.ToolResult, error) {
	return services.ToolResult{Output: "should not run"}, nil
}

func TestPipelineCanVetoBeforeExecute(t *testing.T) {
	s := scope.New(nil)
	_ = s.On("tool.before", func(scope.Event[services.GuardDecision]) error {
		return services.ErrDenied
	})
	wrapped := tools.Pipeline(s, denyTool{})
	_, err := wrapped.Execute(context.Background(), json.RawMessage(`{}`))
	if err == nil || err != services.ErrDenied {
		t.Fatalf("err=%v; want ErrDenied", err)
	}
}
```

Add `var ErrDenied = errors.New("tool denied")` to `services`.

- [x] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/harness/tools -v`
Expected: FAIL with "undefined: tools.Pipeline"

- [x] **Step 3: Implement pipeline and adapter**

`Pipeline` resolves `services.SandboxKey` and `services.ApprovalKey`, fires `tool.before` waterfall, returns `ErrDenied` on veto, then calls `guard.NewTimeout` around `next.Execute`, collects references, and emits `tool.after`. `Adapt` wraps every `agent.Tool` in a small struct implementing `services.Tool`, converting `agent.Reference` to `services.Reference` with identical JSON tags.

- [x] **Step 4: Run test to verify it passes**

Run: `cd server && go test ./internal/harness/tools -v`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add server/internal/harness/tools
git commit -m "feat(harness): add guarded tool pipeline"
```

---

### Task 8: Default agent loop from existing runner

**Files:**
- Create: `server/internal/harness/agentloop/loop.go`
- Create: `server/internal/harness/agentloop/loop_test.go`
- Modify: `server/internal/lib/agent/runner.go` (export adapter hooks only if needed)

**Interfaces:**
- Consumes: `services.AgentLoop`, `services.LLM`, `services.Tools`, `services.Sink`, `services.Event`, scope from prior tasks.
- Produces: `func New(s *scope.Scope) services.AgentLoop`.

- [x] **Step 1: Write failing test**

```go
package agentloop_test

import (
	"context"
	"testing"

	"hsg/internal/harness/agentloop"
	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
)

type stubLLM struct{}

func (stubLLM) MaxTokens() int { return 100 }
func (stubLLM) ChatStream(context.Context, []ai.Message, ai.ChatOptions, func(ai.StreamDelta) error) (ai.ChatResult, error) {
	return ai.ChatResult{Content: "done"}, nil
}

func TestDefaultLoopRunsWithFakeLLM(t *testing.T) {
	s := scope.New(nil)
	_ = s.Provide(services.LLMKey, services.LLM(stubLLM{}), nil)
	loop := agentloop.New(s)
	out, err := loop.Run(context.Background(), services.RunRequest{}, nil)
	if err != nil || out.Content != "done" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}
```

Add the `hsg/internal/lib/ai` import to the test.

- [x] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/harness/agentloop -v`
Expected: FAIL with "undefined: agentloop.New"

- [x] **Step 3: Implement default loop**

Wrap the existing `lib/agent.Runner` logic or port it behind `services.AgentLoop`: read `LLM`, `Tools`, `SystemPrompt`, and `Guards` from scope; preserve existing budget defaults from `Config.withDefaults`; emit `services.AIEvent` values through `Sink`; keep continuation heuristics from `lib/agent/completion.go` as a private policy. Do not change SSE wire tags.

- [x] **Step 4: Run test to verify it passes**

Run: `cd server && go test ./internal/harness/agentloop -v`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add server/internal/harness/agentloop
git commit -m "feat(harness): add default swappable agent loop"
```

---

### Task 9: Mount composition and handler wiring

**Files:**
- Create: `server/internal/harness/mount.go`
- Create: `server/internal/harness/mount_test.go`
- Modify: `server/internal/handlers/chat.go:19` (import), `:433-437` (runner creation), `:462-491` (tool registry adaptation)
- Modify: `server/internal/handlers/knowledge_tool.go:14` (import only if needed)

**Interfaces:**
- Consumes: all prior tasks.
- Produces: `func Mount(gw *ai.Gateway, registry *agent.Registry, cfg MountConfig) (*scope.Scope, error)`, `type MountConfig struct{ Sandbox sandbox.Config; Approval approval.Config; Preset string }`.

- [x] **Step 1: Write failing test**

```go
package harness_test

import (
	"testing"

	"hsg/internal/harness"
	"hsg/internal/lib/agent"
)

func TestMountRegistersServices(t *testing.T) {
	s, err := harness.Mount(nil, agent.NewRegistry(), harness.MountConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if s == nil {
		t.Fatal("nil scope")
	}
	if err := s.Dispose(); err != nil {
		t.Fatal(err)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/harness -v`
Expected: FAIL with "undefined: harness.Mount"

- [x] **Step 3: Implement Mount and wire handler**

`Mount` validates sandbox and approval config, creates root scope, provides the LLM adapter, adapted tools, system prompt, agent loop, and `core.NewAgent` facade. In `chat.go`, replace `agent.NewRunner(...)` with a child scope from an app-scoped `harness.Mount` created at handler construction, override per-request `Sessions`/preset, and call `Agent.Run`. Keep `stream` and `block` signatures accepting the harness Agent so SSE frame generation stays byte-identical.

- [x] **Step 4: Run tests to verify it passes**

Run: `cd server && go test ./internal/harness ./internal/handlers ./internal/lib/agent/... ./internal/lib/ai/... -v`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add server/internal/harness server/internal/handlers/chat.go server/internal/handlers/knowledge_tool.go
git commit -m "feat(harness): mount services into chat handler"
```

---

### Task 10: Golden SSE and billing regression test

**Files:**
- Create: `server/internal/handlers/harness_golden_test.go`
- Modify: `server/internal/handlers/chat_test.go` (reuse helper fixtures only)

**Interfaces:**
- Consumes: handler wiring from Task 9.
- Produces: one golden test comparing SSE frames and billing amounts before/after rewrite.

- [x] **Step 1: Write failing test**

```go
package handlers_test

import (
	"testing"

	"hsg/internal/handlers"
)

func TestHarnessRewriteKeepsSSEAndBilling(t *testing.T) {
	frames, usage, err := handlers.RunHarnessGolden("testdata/harness_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) == 0 || usage.PromptTokens == 0 {
		t.Fatalf("frames=%d usage=%+v", len(frames), usage)
	}
}
```

Create `server/internal/handlers/testdata/harness_golden.json` with the captured frame list and token counts from the existing chat test fixture.

- [x] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/handlers -run TestHarnessRewriteKeepsSSEAndBilling -v`
Expected: FAIL with "undefined: handlers.RunHarnessGolden"

- [x] **Step 3: Implement golden harness**

Add a small exported helper that runs the existing fake chat fixture through the new handler path and returns decoded SSE frames plus usage. Do not alter production SSE behavior to satisfy the test.

- [x] **Step 4: Run test to verify it passes**

Run: `cd server && go test ./internal/handlers -run TestHarnessRewriteKeepsSSEAndBilling -v`
Expected: PASS

- [x] **Step 5: Run full checks and commit**

Run: `cd server && go test ./...`
Run: `make audit` from repo root (gofi layer audit must pass)

```bash
git add server/internal/handlers
git commit -m "test(harness): lock SSE and billing compatibility"
```

---

## Self-Review Notes

Before execution, verify each task compiles with the interfaces from earlier tasks. `Reference` is defined directly as a struct with the same JSON tags as `lib/agent.Reference`. `Task 6` timeout guard must be wired into the pipeline from `Task 7`, and `Task 9` must not change `stream`/`block` SSE tags. No placeholder or TBD remains.
