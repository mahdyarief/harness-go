# DSH Core Parity Implementation Plan (revision of `2026-11-21-dsh-core-followup.md`)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the production harness loop (`server/internal/harness/agentloop`) behave 1:1 with the DeepSeek Harness (DSH) core where harness-go has a consumer, and first restore the safety budgets that were lost when `lib/agent.Runner` was replaced.

**Source of truth:** `D:\Github\agent-workspace\deepseek-harness` @ `2e39e70496aa02986ca6a820c5d497e816b69d7e`. Cite DSH file:line in every port's doc comment.

**Tech Stack:** Go (module `hsg`, `go.mod` says `go 1.27.3`; local toolchain is `go1.26.1` — run `go env GOTOOLCHAIN` once and confirm the toolchain download works before Task 0), stdlib `testing`, `gofmt`, `go vet`.

---

## Why this revision

### Evaluation of the previous plans

| Plan | Status | Verdict |
|---|---|---|
| `2026-11-21-precommit-quality-gate.md` | All tasks `[x]`, commits `6ecbe78..1da7a7e` | Done. No change. |
| `2026-11-21-harness-go.md` | Tasks 1–10 landed in `a128621`/`d8200c0`; boxes never ticked | Done in structure, **not in behaviour** — see gaps below. Its code samples also had compile bugs (`s.On` as a generic method, waterfall test mutating a by-value `ev`) that the implementation fixed silently. |
| `2026-11-21-dsh-core-followup.md` | Not started | **Superseded by this plan.** Ports were correct in isolation but 3 of 4 had no consumer, and it missed live regressions. |

### Problems in the follow-up plan

1. **It ports algorithms that nothing calls.** Task 1 adds failure codes, but nothing maps `ai.ProviderError` to a code and the retry loop does not route on codes. This breaks the plan's own rule: "add only codes with a real consumer". Task 2 (seq-ranges) and Task 4's `ToolPairingBalanced` need a durable session event log, and harness-go has none.
2. **It contradicts itself.** Task 1 says "9 retryChoice entries… (8)". The real numbers are 8 `retryChoice` codes and 5 **default-retried** codes (`llm/llm/src/retry-policy.ts:19-25`). Those are two different sets, and the loop needs the second.
3. **Its event names don't match DSH.** It uses `"tool-call"` / `"assistant-tool-call"`, but DSH uses `tool/call`, `tool/result` and `assistant/message`.
4. **It uses `panic` on corrupt input** (`ToolPairingBalanced`). That is not acceptable in a request path; return an `error`.
5. **It misses the bugs that are live in production right now** (next section).

### Production gaps found in `agentloop/loop.go` (the handler calls it via `harnesspkg.Mount`, `handlers/chat.go:443`)

| # | Gap | Legacy `lib/agent/runner.go` | DSH reference |
|---|---|---|---|
| G1 | `dispatch` truncates `calls` past `MaxToolCalls` but the assistant message keeps **all** `ToolCalls` → unanswered tool_call ids → OpenAI-compatible providers reply 400. After the budget is spent, every later turn dispatches zero calls until `MaxTurns`. | stops with `budget` event (`:290`) | `core/session/src/repair.ts:53` (synthetic results close every call); `agent.ts` closes calls before `step/end` |
| G2 | `MaxDuration` not enforced | enforced | — |
| G3 | `MaxTotalTokens` not enforced; `ChatOptions.MaxTokens` never set | `remaining` passed, `budget` event (`:197`) | `max-tokens` finish is sticky (`agent.ts:374-379`) |
| G4 | Tool goroutines unbounded; `MaxToolConcurrency` ignored | semaphore (`:475`) | `maxParallelToolCalls` 10 (`core/agent-loop/src/constants.ts:6`), ordered commit (`core/tools/src/batch.ts:50`) |
| G5 | `shouldContinue` nudges **conversational** turns ("let me…", trailing `?`) and has no nudge cap; it ignores `MaxContinuationNudges` | nudges only after tools ran, capped; richer heuristics in `completion.go` | `agent/turn-stopping` serial event + `steer()` |
| G6 | Stream-cut resume (`MaxPartialContinuations`) dropped | `:222` | interrupted `assistant/message` |
| G7 | `LoopGuards` (`RepeatedErrorGuard`) capability dropped (it was never enabled in production) | `:298` | — |
| G8 | Retry is linear `150ms·n`, routes on `ProviderError.Retryable` | — | exponential + jitter, routes on code (`retry-policy.ts`, `llm-retry/src/index.ts:60`) |
| G9 | `tools.Pipeline`, `sandbox`, `approval`, `preset` never called; no `tool.before` listener; `approval "never"` means *allow*, but in DSH `never` means *reject any approval request* | — | `core/tools/src/pipeline.ts:191`, `interaction/user-approval/src/index.ts:168` |
| G10 | Truncation keeps only the head of a result; compaction step 1 wipes whole tool outputs | — | head+tail retention (`compaction-tool-result-pruner`: threshold 8192, head 4096, tail 1024) |
| G11 | `compaction` event no longer surfaced to the UI | `:189` | `compaction/prune` |

`loop.go` is 407 LOC, and the pre-commit gate rejects files over 500 LOC, so Task 0 splits it before anything grows.

## Global Constraints

- TDD per task: RED → GREEN → REFACTOR. Watch each new test fail first.
- SSE `Event` JSON tags and billing calls must not change. Every task ends with `go test ./internal/harness/... ./internal/handlers/...` green, including `mount_golden_test.go`.
- New event `Type` strings must match the legacy ones (`budget`, `notice`, `compaction`) so the web UI renders them. Do not invent new ones.
- Route on `services.ErrorCode(err)`, never on message text.
- Model-visible text is in Indonesian, matching the existing strings. Codes are SCREAMING_SNAKE and copied verbatim from DSH.
- Files ≤500 LOC; `gofmt`; `python scripts/pre_commit_check.py files`; `trail check`; Conventional Commit per task.
- `lib/ai` stays the egress boundary: it must never expose provider body text upward (see Decision D1).

## Decisions to confirm before execution

- **D1 – Context-overflow / quota detection needs the provider body.** `lib/ai` drops the body on purpose (`chat.go:80`).
  - **Recommended:** classify *inside* `lib/ai` and add only `ProviderError.Code string`. The regex runs on the body, and only the code leaves the package.
  - **Alternative:** status-only mapping. No overflow recovery is possible this way; a 400 is ambiguous.
- **D2 – Approval semantics.** Adopt DSH's: tools that are not read-only *ask*. `ask` + no answerer gives `APPROVAL_UNAVAILABLE` (fail-closed); `never` gives `APPROVAL_REJECTED`. All current tools are `ReadOnly()==true` (`handlers/chat.go:804,830`, `knowledge_tool.go:34`), so production behaviour does not change.
- **D3 – Summarization compaction** (DSH `compaction-basic`) costs an extra LLM call that billing must account for. It is deferred to Phase C until billing signs off.

---

## Phase A — Restore parity and fix protocol bugs

### Task 0: Split `loop.go` without behaviour change

**Files:** create `agentloop/dispatch.go` (dispatch, toolError, validateArgs, canonicalArgs), `agentloop/compact.go` (compact, estimateTokens), `agentloop/truncate.go` (truncate), and `agentloop/continuation.go` (shouldContinue, continuationPrompt). `loop.go` keeps `Run`, `call` and `accumulate`.

- [ ] Move the code verbatim; there is no logic change.
- [ ] `cd server && go test ./internal/harness/... ./internal/handlers/...` → PASS (unchanged count).
- [ ] Commit `refactor(harness): split agent loop into focused files`.

### Task 1: Close every tool call; stop on tool budget (G1)

**Files:** `agentloop/dispatch.go`, `agentloop/loop.go`, test `agentloop/budget_test.go`.
**DSH:** `core/session/src/repair.ts:15-41` (codes and wording), `core/tools/src/batch.ts` (skipped calls get synthetic results).

**Produces:**
- `const CodeToolNotStarted = "TOOL_NOT_STARTED"` in `services`.
- `services.ErrToolBudget` (a `HarnessError` with code `TOOL_BUDGET_EXHAUSTED`).

- [ ] **Step 1: Failing test.** Use `MaxToolCalls: 2` and an LLM that asks for 3 calls on turn 1.
  - Every `ToolCall.ID` in the assistant message has exactly one `role:"tool"` message. The third message's content is the not-started text.
  - The loop then returns `services.ErrorCode(err) == "TOOL_BUDGET_EXHAUSTED"` and `out.Partial == true`.
  - The sink receives `{Type:"budget", Error:"tool budget exhausted"}`.

```go
func TestDispatchAnswersEveryCallAndStopsOnToolBudget(t *testing.T) {
	llm := &recordingLLM{script: []ai.ChatResult{{
		FinishReason: "tool_calls",
		ToolCalls: []ai.ToolCall{call("a"), call("b"), call("c")},
	}, {Content: "tidak boleh tercapai", FinishReason: "stop"}}}
	s := mountLoop(t, llm, &fakeTool{}, services.LoopConfig{MaxToolCalls: 2})
	var events []services.AIEvent
	out, err := agentloop.New(s).Run(context.Background(), services.RunRequest{Messages: userMsg("x")},
		func(e services.AIEvent) error { events = append(events, e); return nil })
	if services.ErrorCode(err) != "TOOL_BUDGET_EXHAUSTED" || !out.Partial {
		t.Fatalf("err=%v partial=%v", err, out.Partial)
	}
	if llm.calls != 1 {
		t.Fatalf("model called %d times after budget", llm.calls)
	}
	assertEveryCallAnswered(t, llm.lastHistory) // helper: ids in assistant msg == ids in tool msgs
	assertHasEvent(t, events, "budget")
}
```

  Put the shared helpers (`recordingLLM`, `mountLoop`, `call`, `userMsg`, `assertEveryCallAnswered`, `assertHasEvent`) in `agentloop/helpers_test.go`. `recordingLLM` stores a copy of the history it receives.
- [ ] **Step 2:** run it and watch it fail (the model is called again with dangling ids).
- [ ] **Step 3: Implement.**
  - In `dispatch`, calls past the budget get `toolError(call, notStartedText)` plus a `ToolRecord{Err: CodeToolNotStarted}`. Never slice `calls`.
  - `notStartedText` = "Panggilan alat tidak dijalankan karena batas jumlah panggilan alat tercapai. Selesaikan jawaban dengan informasi yang ada." (This adapts DSH `repair.ts:39`.)
  - After `dispatch`, if `out.ToolCalls >= cfg.MaxToolCalls` and any call was skipped: emit `budget`, return `ErrToolBudget`.
- [ ] **Step 4:** PASS. Also add `assertEveryCallAnswered` to the existing deny, timeout and unknown-tool tests.
- [ ] **Step 5:** commit `fix(harness): answer every tool call and stop on tool budget`.

### Task 2: Enforce duration and token budgets (G2, G3)

**Files:** `agentloop/loop.go`, test `agentloop/budget_test.go`.

- [ ] **Failing tests:**
  1. `MaxDuration: 50ms` with an LLM that blocks until ctx is done → returns within ~100 ms with `errors.Is(err, context.DeadlineExceeded)` and `Partial`.
  2. `MaxTotalTokens: 10` with an LLM that reports `CompletionTokens: 11` on a tool turn → `budget` event, code `TOKEN_BUDGET_EXHAUSTED`, and no second model call.
  3. The LLM receives `opts.MaxTokens == MaxTotalTokens - completionSoFar` on each call.
  4. `FinishReason: "length"` → `out.Partial = true`, and the next tool turn does not clear it (sticky, `agent.ts:374-379`).
- [ ] **Implement:**
  - `ctx, cancel := context.WithTimeout(ctx, cfg.MaxDuration)` at the top of `Run`.
  - Keep `outerCtx` for the client-abort check (see the legacy comment at `runner.go:219`).
  - Compute `remaining` before each call, and bail with the legacy message strings.
- [ ] Commit `fix(harness): enforce duration and token budgets`.

### Task 3: Bounded, ordered tool concurrency (G4)

**Files:** `agentloop/dispatch.go`, test `agentloop/concurrency_test.go`.
**DSH:** `core/tools/src/batch.ts:186-199`. Consecutive concurrency-safe calls share a pool and the others act as barriers. Here `ReadOnly()` is the concurrency-safe flag.

- [ ] **Failing test:** use `MaxToolConcurrency: 2` and 5 read-only calls whose tool tracks peak in-flight with an atomic counter, then assert peak ≤ 2. Add one non-read-only tool in the middle and assert it ran alone (peak 1 during it). Assert that tool messages come out in model order.
- [ ] **Implement:**
  - Group `jobs` into runs: consecutive `ReadOnly()` jobs form one group, and each non-read-only job is a group of its own.
  - Each group runs with a semaphore `make(chan struct{}, cfg.MaxToolConcurrency)`, and the next group waits for it.
  - Results are already written by index, so order is preserved.
- [ ] Commit `fix(harness): bound tool concurrency and serialize write tools`.

### Task 4: Port continuation policy and partial-stream resume (G5, G6)

**Files:**
- `agentloop/continuation.go`: port `lib/agent/completion.go` verbatim (it is 240 LOC of tested heuristics, plus its test file).
- `agentloop/loop.go`.
- tests `agentloop/continuation_test.go` (copy `lib/agent/completion_test.go`) and `agentloop/resume_test.go`.

- [ ] **Failing tests:**
  1. A turn with no tools that answers "Saya akan jelaskan: …" is **not** nudged. This changes the current `TestAntiStallContinuationRunsAnotherTurn`: rewrite that test so tools run first.
  2. After tools, with `MaxContinuationNudges: 1` and a model that stalls twice → exactly one nudge, then return. The first nudge emits a `notice` with the legacy text.
  3. A stream returns `Content:"setengah"` plus a `*ai.ProviderError` → loop appends assistant + "Lanjutkan jawaban dari titik terakhir…", emits `notice`, and the final `out.Content == "setengah" + rest`. With `MaxPartialContinuations: 1` and two cuts → `Partial` with the carried text.
- [ ] **Implement:** mirror `runner.go:212-276`, using separate counters `nudges` and `partialContinuations`. Use the legacy nudge prompt text, not the current `continuationPrompt`.
- [ ] Commit `fix(harness): restore continuation policy and stream resume`.

### Task 5: Loop guards (G7)

**Files:** `harness/guard/loopguard.go` (port the `LoopGuard`/`Verdict`/`Memory` contract from `lib/agent/guard.go` and `RepeatedErrorGuard` from `guard_repeat.go`), `services.LoopConfig.LoopGuards []guard.LoopGuard`, wired in `loop.go`. Tests are copied from `lib/agent/guard_repeat_test.go`, plus a loop test.

> Check for an import cycle: `services` must not import `guard`. Instead put the `LoopGuard` interface and `Verdict` in `services`, and keep the implementations in `guard`.

- [ ] **Failing loop test:** the same tool returns the same error 3 times → the run ends gracefully, `out.Content == verdict.Reason`, `err == nil`.
- [ ] **Implement:**
  - Memory is per run.
  - Inspect after each tool turn.
  - The legacy handler never passed `LoopGuards`: `git log -S LoopGuards` shows only `a128621`, and production never wired it. So G7 is a lost *capability*, not a production regression.
  - `Mount` exposes `MountConfig.LoopGuards`, defaulting to empty. Enabling `RepeatedErrorGuard` in `chat.go` is a separate, explicit commit.
- [ ] Commit `feat(harness): restore loop guards`.

### Task 6: Wire one ordered pre-execute pipeline (G9)

**Files:** `harness/tools/pipeline.go` (rewrite), `harness/approval/policy.go`, `harness/preset/preset.go`, `harness/mount.go`, `agentloop/dispatch.go`. Tests are in each package plus `mount_test.go`.
**DSH:** `core/tools/src/pipeline.ts:191-356`, `contracts.ts:375` (`PreToolDecision`), `error-codes.ts:12-18`.

**Produces:**
- `type PreDecision struct{ Kind string /* allow|deny|ask */; Reason, Code string }`
- `type PreExecute func(ctx context.Context, call ToolInvocation, next func() PreDecision) PreDecision`, registered on `scope` as a waterfall with an explicit `next`.
- `Pipeline` order: preset allow-list → sandbox (`!ReadOnly()` and mode `read-only` gives deny `SANDBOX_READ_ONLY`) → `ask` resolution via approval (`never` gives `APPROVAL_REJECTED`; `ask` with no answerer gives `APPROVAL_UNAVAILABLE`) → user `tool.before` listeners → timeout → `Execute` → result retention (Task 9) → `tool.after`.

- [ ] **Failing tests:**
  - Read-only tool under the `strict` preset runs.
  - Write tool under `read-only` is denied with the code and Indonesian reason as tool-result text.
  - A tool outside the allow-list is denied.
  - Mount with no preset gives `read-only` + `never`.
- [ ] **Implement:**
  - Delete the duplicated inline waterfall in `dispatch`; dispatch calls `Pipeline(...).Execute`.
  - `Mount` takes `MountConfig{Preset string; Sandbox sandbox.Config}`, validates with `sandbox.Validate`, and applies the preset.
- [ ] Golden test still PASS (all prod tools are read-only, per D2).
- [ ] Commit `feat(harness): wire sandbox, approval and preset into tool pipeline`.

## Phase B — DSH core ports with real consumers

### Task 7: LLM failure codes and provider classification (G8 prerequisite)

Revises follow-up Task 1.

**Files:** `harness/llm/failure.go` + test; `lib/ai/chat.go` (adds `Code` only, per D1); `harness/llm/adapter.go`.
**DSH:** `llm/llm/src/error.ts:17-71,141-175`, `adapter-failure.ts:17`, `llm-deepseek/src/transport.ts:41-47`.

- [ ] Keep the follow-up plan's `FailureCode` constants, regexes and tests. They are correct: every case was checked against the RE2 syntax. Two changes:
  - Drop `CodeAccountQuota` and `CodeInvalidCredential` (no consumer).
  - Add `CodeAuth = "AUTH"`, `CodeInvalidRequest = "INVALID_REQUEST"`, and `HTTPCode(status) = fmt.Sprintf("HTTP_%d", status)`.
- [ ] Add `DefaultRetryableCodes = {EMPTY_RESPONSE, RATE_LIMIT, SERVER, TIMEOUT, TRANSPORT}` (5, `retry-policy.ts:19-25`). Do **not** export a separate `retryChoice` set; it has no consumer.
- [ ] Add `func ClassifyHTTP(status int, detail string) FailureCode`, checking in this order:
  1. 401/403 → AUTH
  2. quota regex or 402 → QUOTA
  3. 429 → RATE_LIMIT
  4. context regex → CONTEXT_WINDOW_EXCEEDED
  5. 400/413 → INVALID_REQUEST
  6. ≥500 → SERVER
  7. otherwise `HTTP_n`

  Table test each row, including "400 + context wording → CONTEXT_WINDOW_EXCEEDED".
- [ ] `lib/ai`:
  - Set `ProviderError.Code = string(llm.ClassifyHTTP(status, body[:min(len,4096)]))`. To avoid an import cycle, put `ClassifyHTTP` in `lib/ai/classify.go` and have `harness/llm` re-export the constants.
  - Map `ctx.DeadlineExceeded` → TIMEOUT, network errors → TRANSPORT, empty stream → EMPTY_RESPONSE.
  - `security_test.go` must still prove that no body text reaches `Error()`.
- [ ] The adapter wraps every gateway error with `NewFailure(msg, code, err)`.
- [ ] Commit `feat(harness): classify provider failures into DSH codes`.

### Task 8: DSH retry policy (G8)

**Files:** `harness/llm/retry.go` + test; `agentloop/loop.go` `call`.
**DSH:** `llm/llm/src/retry-policy.ts:15-25,155`, `llm/llm-retry/src/index.ts:60,129`.

- [ ] **Failing tests**, with an injected `sleep func(ctx, d) error` and a seeded `rand`:
  - `Delay(1)=500ms±10%`, `Delay(2)=1s±10%`, and `Delay(10)` capped at 10 s.
  - Retry on `RATE_LIMIT`; no retry on `AUTH`, `QUOTA` or `CONTEXT_WINDOW_EXCEEDED`.
  - No retry once a delta was emitted (keep the existing rule).
  - Retry count resets per step.
- [ ] **Implement:**
  - `type RetryPolicy struct{ MaxRetries int; Initial, Max time.Duration; Jitter float64; Codes map[string]bool }`.
  - `call()` routes on `services.ErrorCode(err)`.
  - Keep `LoopConfig.MaxRetries` (bounded 0–3) as the cap instead of DSH's 5; note this in the doc comment.
- [ ] Commit `feat(harness): port DSH exponential retry policy`.

### Task 9: Head+tail result retention and tool-result pruning (G10, G11)

Revises follow-up Task 4.

**Files:** `harness/core/retention.go` + test, `agentloop/truncate.go`, `agentloop/compact.go`.
**DSH:** `util/output-retention` (TextRetainer head+tail), `compaction/compaction-tool-result-pruner/src/config.ts` (8192/4096/1024 and the fixed marker), `compaction/compaction/src/tool-pairing.ts:112`.

- [ ] **Failing tests:**
  - `RetainHeadTail(s, head, tail)` keeps both ends, never splits a rune, and inserts the marker `[... tool result middle pruned ...]` with the omitted byte count.
  - Under the cap it returns the input unchanged.
  - The existing `truncate_test.go` cases are updated to expect head+tail.
- [ ] **Failing test:** `ToolPairingBalanced(history []ai.Message, cut int) (bool, error)`, which works on **messages** (harness-go has no event log).
  - It counts `+len(ToolCalls)` per assistant and `-1` per `tool`.
  - It returns an `error` (not `panic`) when a tool result has no call.
- [ ] Compaction:
  - Step 1 prunes the middle of tool messages older than `KeepLastMessages` instead of wiping them.
  - The drop step's cut must satisfy `ToolPairingBalanced`.
  - Emit `{Type:"compaction", Text: describe()}` as the legacy runner did (`runner.go:189`, `:545`).
- [ ] Commit `feat(harness): port DSH head+tail retention and pairing-safe compaction`.

### Task 10: Context-overflow recovery (needs D1)

**Files:** `agentloop/loop.go`, test.
**DSH:** `compaction-basic` trigger (c), `_registerAutomaticCompaction` `:162`; `maxOverflowRetries`.

- [ ] **Failing test:** the first call fails with code `CONTEXT_WINDOW_EXCEEDED`. The loop then force-compacts (target `ContextTokenCap/2`), retries once, and succeeds. If the second call overflows again, return the error.
- [ ] Implement it with an overflow counter that resets after a successful assistant message.
- [ ] Commit `feat(harness): recover from context-window overflow`.

### Task 11: Token estimator parity

**Files:** `agentloop/compact.go` `estimateTokens` + test.
**DSH:** `llm/token-meter/src/estimate.ts:13,19,99`, `index.ts:151`.

- [ ] **Failing test:** the estimate equals `Σ(len/4 + ROLE_OVERHEAD 4)` plus the tool schema JSON `/4`. When the last call reported usage, the baseline is `PromptTokens + CompletionTokens` of that call plus an estimate of only the messages appended since.
- [ ] Commit `feat(harness): port DSH token estimator with usage baseline`.

## Phase C — Deferred (each needs a consumer or a decision first)

| DSH feature | Why deferred | Unblocked by |
|---|---|---|
| Summarization compaction (`compaction-basic` `region.ts:118,174`, `summarizer.ts:35-73`) | Extra LLM call that billing must account for | D3 sign-off; then port `selectCompactableRange` and the 8-section summary prompt verbatim |
| Session event log (`types.ts:291-440`), `seq-ranges.ts`, event-log `repair.ts`, fork | harness-go persists messages, not events; nothing reads them | A durable audit/replay requirement. Follow-up Tasks 2–3 code can then be reused with DSH event names |
| System-prompt sections (`core/system-prompt` `SECTION_ORDERS`), runtime-context message | One static prompt today | A second prompt contributor (persona, sandbox/approval policy sentence) |
| `repeat-tool-reminder` reset on a new user message (`guard/repeat-tool-reminder/src/index.ts:228`) | Loop runs one user turn per request | Multi-turn sessions inside one run |
| Spill store (`spill/spill-policy`) | No retrieval tool | A `read_spill` tool |
| Hooks, MCP, subagents, goal rounds, sandbox runners, PTC | Platform/I-O features outside the chat API scope | Product request |

## Final verification

```bash
cd /d/Github/agent-workspace/harness-go/server
gofmt -l ./internal/harness && go vet ./internal/harness/... && go test ./...
cd .. && python scripts/test_pre_commit_check.py && make audit && trail check
```

Manual smoke test (skill `run`): one streamed chat with web search and one with knowledge search. Confirm that the SSE frames, `budget`/`notice`/`compaction` events and billing rows match pre-change captures.

## Self-review

- Every task cites a gap (G1–G11) or a DSH file:line, and names its consumer.
- No task changes SSE tags, billing calls, schema or the UI.
- Task order respects dependencies:
  - 0 before everything
  - 7 → 8 → 10
  - 9 before 10
  - 6 after 1 and 3, because dispatch is rewritten again
