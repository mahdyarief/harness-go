# harness-go design

Date: 2026-11-21

## Goal

Create `harness-go` from `mahdyarief/hsg@feat/foundation` and rewrite the Go
agent harness using DeepSeek Harness (DSH) architecture as a design reference.
The result is a native Go harness with Cordis-style service boundaries and
DSH-inspired guardrails. DSH runtime, Node, and sidecars are not used.

## Scope

- Module path stays `hsg` so existing imports continue to resolve.
- No changes to React UI, Authula, tenant RLS, database schema, or migrations.
- Billing and SSE contracts stay compatible; handler/SSE changes are allowed only
  when the old contract cannot be preserved.
- Existing deployment-owned configuration stays in `cmd/api/foundation_config.go`.
- The old `server/internal/lib/agent` package is replaced; `server/internal/lib/ai`
  remains the provider transport and egress-security boundary.

## Architecture

A new package `server/internal/harness/` owns the rewritten harness.

| DSH / Cordis concept | harness-go implementation |
|---|---|
| plugin with `inject` dependencies | `scope/`: typed `Key[T]`, `Provide`, `Require` with parent fallback, and `Register` returning a `Disposer` |
| context as a repository of services | `Scope`, created app-wide at startup and as a child per request |
| typed events | `scope/events.go`: `emit`, `waterfall` for policy interception, and `bail` for guard veto; `serial` and `parallel` are intentionally omitted |
| `ctx.tools`, `ctx.llm`, `ctx.sessions`, `ctx.systemPrompt` | interfaces and typed keys under `harness/services/`; no concrete implementations |
| `ctx.agents` vs `ctx.agentLoop` | `services.Agent` is the public handle; `services.AgentLoop` is the swappable driver. Extensions depend on `Agent`, not on `agentLoop` |
| service lookup | `harness/core/agent.go` resolves `AgentLoop` from the scope |

`harness.Mount(ctx, cfg) (*Scope, error)` is the single composition entrypoint.
It registers every service and returns a scope whose `Dispose()` calls disposers
in LIFO order and joins errors with `errors.Join`.

## Mapping from existing code

- `lib/agent/runner.go`: loop, budgets, and compaction become `harness/agentloop/loop.go`.
  Compaction and continuation are extracted as replaceable policies.
- `lib/agent/completion.go`: becomes `harness/agentloop/continuation.go`, a
  replaceable continuation policy. Existing heuristics are preserved.
- `lib/agent/tool.go`: tool interfaces and result types move to
  `harness/services/tools.go`; registry and guarded execution move to
  `harness/tools/pipeline.go`. `Reference` and `ToolResult` keep wire compatibility
  through type aliases.
- `lib/ai/gateway.go`: unchanged. `harness/llm/adapter.go` exposes it as
  `services.LLM`.
- Session persistence: `services.Sessions` wraps the existing conversation and
  message repositories behind an interface. No repository signature or schema
  changes are needed; the child scope injects the conversation ID per request.
- Chat handler: imports the harness scope and calls `Agent.Run`. SSE frames and
  billing calls remain unchanged unless a golden test proves otherwise.

## Turn flow

1. The chat handler creates a child scope from the app scope. It overrides
   request-scoped services: session keyed by conversation ID, permission preset,
   tool allow-list, approval policy, and sandbox policy.
2. `Agent.Run` resolves `AgentLoop` from its scope and runs one turn.
3. `services.LLM` streams model output. An `llm` event hook observes deltas
   without making the loop depend on logging or transport details.
4. Tool calls enter the tools pipeline in this order: `BeforeToolCall` waterfall,
   sandbox check, approval check, repeat-tool guard, timeout guard, `Execute`,
   result truncation, `AfterToolCall` event, session append, and reference
   collection.
5. Denials become typed errors (`ErrDenied`, `ErrApprovalRejected`) that are fed
   back to the model as tool-result text, so the turn can recover. Provider and
   host errors keep their existing safe 422 mapping in the handler.

## Guardrails

- `harness/sandbox`: `mode` defaults to `read-only`; `workspace-write` is explicit
  opt-in. `workspaceRoot` must be an absolute path and is validated at `Mount`;
  relative values fail to load. `additionalWritableRoots` is optional.
- `harness/approval`: policy is `ask` or `never`. The chat API defaults to `never`;
  `ask` emits a structured clarification event and never blocks a request.
- `harness/guard/repeattool.go`: detects an exact repeated tool call and emits a
  reminder to the model.
- `harness/guard/timeout.go`: enforces a per-tool deadline and converts expiry
  into a tool error.
- `harness/preset`: maps a named permission preset to tool allow-list, sandbox
  mode, and approval policy. Every guard is replaceable or removable through the
  scope, mirroring the DSH base bundle.

## Error handling and testing

- `Dispose` joins cleanup errors with `errors.Join` and always runs every
  disposer once.
- Sandbox and approval denials are logged as structured events and are never
  exposed as raw internal errors to the model or client.
- Tests are table-driven and cover: scope provide/require/dispose ordering,
  parent fallback, event dispatch modes used here (`emit`, `waterfall`, `bail`),
  repeat-tool reminder, tool timeout, sandbox validation, approval outcomes,
  and permission preset composition.
- Loop tests use a fake `LLM` and fake tools to verify budgets, continuation
  nudges, compaction, and swapping in a fake `AgentLoop`.
- A golden test replays the current chat flow and compares SSE frames and
  billing amounts. `make audit` and the gofi layer audit must pass; new packages
  import neither Fiber nor `database/sql`, and session persistence uses a
  repository interface.

## Done criteria

- The AgentLoop driver can be swapped without changing consumers.
- All guards have passing tests.
- Sandbox defaults to `read-only`.
- Golden SSE and billing output is unchanged.
- No React, migration, or schema changes are required.
