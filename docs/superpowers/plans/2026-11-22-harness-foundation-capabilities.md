# Harness Foundation Capabilities Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add general-purpose, domain-agnostic capabilities to harness-go so that any multi-agent, auditable, human-in-the-loop application can be built on it. These are the capabilities DeepSeek Harness (DSH) offers as packages, and the reason harness-go can replace DSH as the runtime.

**First consumer:** `2026-11-22-multi-agent-claim-verification.md`, the BPJS claim verification thesis. That plan holds only domain code (claim schema, the 6 agent prompts, the rule catalog, KG data, the evaluation dataset). Everything generic lives here.

**Hard rule: nothing in this plan may mention claims, BPJS, ICD or INA-CBG.**
- Package names, types, codes and tests are generic.
- The examples use neutral domains (e.g. "document review", "invoice check").
- Review rejects any task that leaks domain terms into `server/internal/harness/...`.

**DSH reference:** `D:\Github\agent-workspace\deepseek-harness` @ `2e39e70496aa02986ca6a820c5d497e816b69d7e`. Cite the DSH package in the doc comment of every port.

**Tech Stack:**
- Go module `hsg`; the existing harness (`scope`, `services`, `agentloop`, `tools`, `llm`, `guard`, `preset`, `sandbox`)
- `lib/ai` (gateway), `lib/kb` (Hybrid RAG)
- PostgreSQL 18 + RLS per tenant

**Status: DONE** (implemented 2026-11-23, review fixes 2026-11-24). See "Execution status" at the end of this file.

---

## Capability map

| # | Capability | DSH counterpart | harness-go today | Task |
|---|---|---|---|---|
| C1 | Full JSON Schema validation for tool arguments | `core/tools` (zod schema) | key check only (`agentloop.validateArgs`) | 1 |
| C2 | Structured output: a run ends with a typed, validated value | `tool-present`, structured result | text only (`Outcome.Content`) | 2 |
| C3 | Agent profiles: named agent = prompt + tools + preset + budget | `preset/agent-preset`, `subagent-preset-composition` | one global `Mount` per request | 3 |
| C4 | Team orchestration: parallel isolated children, deterministic join | `subagent-spawn-in-process`, `team/agent-team` | none | 4 |
| C5 | Result reducers: combine child results by rule | (custom in each DSH workflow) | none | 4 |
| C6 | Run event log: append-only, replayable, per run | `session/session-persistence-*`, `session-projection` | `ToolRecord` in memory only; `services.Sessions` has no implementation | 5 |
| C7 | Evidence model: a typed provenance for every claim the model makes | `deliverable-file-references` | `services.Reference` (KB passages only) | 6 |
| C8 | Human review inbox: a durable queue of items awaiting a person's decision | `interaction/user-questions`, `user-approval` | tool approval only, `ApprovalAnswer` not wired | 7 |
| C9 | Declarative rule engine as a tool | — (DSH has none; needed for deterministic checks) | none | 8 |
| C10 | External service tool: HTTP call under an egress policy | `connector/*`, `mcp` (narrower here) | `lib/kbembed` pattern only | 9 |
| C11 | Knowledge filters: metadata + validity period on retrieval | — | `lib/kb` without temporal filters | 10 |
| C12 | Determinism + LLM record/replay | `test-support` replay fixtures | none | 11 |
| C13 | Egress transforms: redact identifiers before the model | `guard` hooks | none | 12 |
| C14 | Cost accounting per agent/run (price per model) | `session-stats` | tokens only | 13 |
| C15 | Experiment harness: run N configurations on one dataset, compute metrics | `benchmarks/*` | `lib/kbeval` (retrieval only) | 14 |

**Not in scope:**
- computer-use, browser, shell, ssh, the sandbox runner, PTC and `workflow` scripts, goal rounds, MCP, summarizer compaction, OTel.
- These capabilities have no caller. Add one only when a consumer needs it (`.trail` learning: port DSH features only when they have a caller).

## Global Constraints

- TDD per task: RED → GREEN → REFACTOR, and watch each test fail first. Conventional Commit per task.
- Files ≤500 LOC; `gofmt`; `python scripts/pre_commit_check.py files`; `trail check` before commit and push.
- **Backward compatibility:**
  - the existing chat (`handlers/chat.go` → `harness.Mount`) must not change;
  - SSE event JSON stays legacy, and new fields are `omitempty`;
  - `mount_golden_test.go` and all existing tests stay green.
- New capabilities are **opt-in through the scope** (`scope.Provide` a new key). Without the key, behavior is identical to today.
- Every storage capability is an interface in `services` with two implementations:
  - in-memory, for tests and experiments without a database;
  - Postgres, with RLS per tenant (pattern in `migrations/000006_tenants.up.sql`).
  - New migrations continue from `000040`.
- Fake LLMs in tests fail the way the real gateway fails (`.trail` learning).
- `lib/ai` stays the only egress. Provider body text is never exposed upward.
- Model-facing text is in Indonesian. Codes are SCREAMING_SNAKE.

## Decisions to confirm before execution

**F1 — Structured output through a terminal tool call.**
- A typed result is submitted by a tool marked `Terminal`, whose parameters are the result schema.
- Reasons:
  - It works on every provider that supports tool calls (DeepSeek: `json_object` only, no strict schema).
  - `chatBody` (`lib/ai/chat.go:194`) already sends a named `tool_choice`.
- `response_format` is not added now (no caller).

**F2 — Orchestration is deterministic code, not model delegation.**
- `team` runs a set of members chosen by the caller.
- Delegation by the model (DSH `tool-subagent`) can be added later as a tool on top of `team`, without changing it.

**F3 — Child isolation through `scope.Child()`.**
- Each member gets its own child scope: tools, allowlist, LoopConfig, history.
- It inherits only the LLM and the log/inbox stores from the parent.
- One child's failure or budget never touches the others.

**F4 — The run log is the source of truth for audit; projections are derived.**
- Events are appended with a contiguous `seq` and never updated.
- Reports, metrics and review views are rebuilt from the log. (DSH: session log + projection.)

**F5 — JSON Schema subset written in-house**, with no dependency:
- `type`, `enum`, `const`, `required`, `properties`, `additionalProperties`, `items`, `minItems`/`maxItems`, `minimum`/`maximum`, `minLength`/`maxLength`, `pattern`.
- Enough for tool and result schemas. Anything outside the subset is rejected when the tool is registered (fail at startup, not at runtime).

---

## Phase 1 — Agent composition

### Task 1: JSON Schema validator for tools

**Files:**
- Create: `server/internal/harness/schema/schema.go`, `schema_test.go`
- Modify: `server/internal/harness/agentloop/dispatch.go` (replace `validateArgs`), `harness/tools` (validate the schema at registration)

- [ ] Test (RED), table-driven: each keyword in F5 passes and fails correctly; nested objects and arrays; an unknown keyword → `Compile` error `SCHEMA_UNSUPPORTED`.
- [ ] `schema.Compile(json.RawMessage) (*Schema, error)` and `(*Schema).Validate(json.RawMessage) []Violation{Path, Reason}`.
- [ ] A dispatch rejection returns a tool message listing the violated paths (Indonesian), so the model can fix its arguments. The record carries `Err: "TOOL_ARGS_INVALID"`.
- [ ] Every existing tool schema (`knowledge_tool`, `exa`, test fakes) must compile. If not, fix the schema, not the validator.
- [ ] Commit: `feat(harness): validate tool arguments against their JSON schema`.

### Task 2: Terminal tool and structured outcome

**Files:**
- Create: `server/internal/harness/structured/structured.go`, `structured_test.go`
- Modify: `services/services.go` (`Terminal` interface, `Outcome.Structured json.RawMessage`), `agentloop/loop.go`

- [ ] `services.Terminal` (optional): `Terminal() bool`. When a terminal call succeeds, the loop ends **without** another model turn.
  - Its output (validated JSON) goes into `Outcome.Structured`.
  - The other calls in the same batch are still answered (pairing stays valid).
- [ ] `structured.Submit(name, description string, schema json.RawMessage) services.Tool` is a generic terminal-tool factory.
- [ ] `LoopConfig.RequireStructured string` (tool name):
  - the model answers in text without calling it → one nudge, then `STRUCTURED_MISSING`;
  - invalid arguments → repaired up to `MaxStructuredRepairs` (default 2), then `STRUCTURED_INVALID`.
- [ ] Tests (RED):
  - a valid submit → `Turns==1`, `Structured` filled, the LLM is not called again;
  - invalid then valid → 2 turns;
  - text with no submit → `STRUCTURED_MISSING`;
  - without `RequireStructured` → identical to today (golden test).
- [ ] `loop.go` is now 281 LOC. If it nears 450, move the terminal logic to `agentloop/terminal.go`.
- [ ] Commit: `feat(harness): end runs with a validated structured result`.

### Task 3: Agent profiles

DSH counterpart: `preset/agent-preset`.

**Files:**
- Create: `server/internal/harness/profile/profile.go`, `profile_test.go`
- Modify: `server/internal/harness/mount.go` (`MountProfile(parent *scope.Scope, p Profile)`)

```go
type Profile struct {
    Name       string
    System     string              // system prompt text
    Tools      []string            // allowlist over the parent's registry
    Result     *ResultSpec         // optional: terminal tool name + schema (Task 2)
    Preset     string              // read-only | workspace | ...
    LoopConfig services.LoopConfig // budgets for this agent
}
func (p Profile) Hash() string     // sha256 of System+Tools+Result schema+LoopConfig, logged per run
```

- [ ] `MountProfile` creates `parent.Child()`. It provides the profile's allowlist, `SystemPromptKey`, LoopConfig and terminal tool, and inherits the parent's LLM, registry and stores (F3).
- [ ] Tests:
  - an allowlist that names a tool missing from the registry → error at mount;
  - two profiles on one parent do not share history or budget;
  - the same profile → the same `Hash()`; changing one prompt character → a different hash.
- [ ] Profiles can be loaded from embedded files (`//go:embed`, YAML front-matter + markdown body), so domain prompts live outside Go code.
- [ ] Commit: `feat(harness): add agent profiles mounted in child scopes`.

### Task 4: Team orchestration and reducers

DSH counterparts: `subagent-spawn-in-process`, `team/agent-team`.

**Files:**
- Create: `server/internal/harness/team/team.go`, `reduce.go`, `team_test.go`, `reduce_test.go`
- Modify: `services/services.go` (`AIEvent.Agent string \`json:"agent,omitempty"\``)

```go
type Member struct { Profile profile.Profile; Input services.RunRequest }
type MemberResult struct {
    Name      string
    Outcome   services.Outcome
    Err       error
    ErrCode   string // AGENT_TIMEOUT, AGENT_FAILED, STRUCTURED_INVALID, ...
}
type Config struct { MaxParallel int; MemberTimeout time.Duration }
func Run(ctx context.Context, parent *scope.Scope, members []Member, cfg Config, sink services.Sink) (Result, error)
```

- [ ] Members run in parallel up to `MaxParallel`, each with its own deadline. A failure in one member does not cancel the others. Results come back in input order.
- [ ] Each member's events go to the sink with `Agent` filled (the chat UI ignores it because of `omitempty`).
- [ ] `Result` sums the usage of every member (tokens, cost from Task 13, per-member and total duration).
- [ ] `reduce.go`: generic reducers over `[]MemberResult`.
  - `reduce.Rules[T]` is an ordered list of `(name, predicate, output)`. The first rule that matches wins.
  - It returns the result **and the name of the rule that decided** (audit, Task 5).
  - No LLM involved.
- [ ] Tests (RED):
  - 3 fake members, one hangs past its deadline → 2 results come back, the hung one has `AGENT_TIMEOUT`, total duration ≈ `MemberTimeout`;
  - result order is stable across 50 runs;
  - a reducer with 3 rules → the expected rule wins and its name is returned;
  - cancelling the parent context stops every member.
- [ ] Commit: `feat(harness): run agent teams in isolated scopes` and `feat(harness): add rule-ordered result reducers`.

**Checkpoint A:** several profiles run in parallel, each returns a valid structured result, and a reducer combines them. Demo with a neutral domain in `team/example_test.go`.

---

## Phase 2 — Provenance, audit, people

### Task 5: Run event log

DSH counterparts: `session-persistence-jsonl` / `-sqlite`, `session-projection`.

**Files:**
- Create: `server/internal/harness/runlog/runlog.go` (interface + event types), `memory.go`, `replay.go`, tests
- Create: `server/internal/repositories/runlog.go`, `migrations/000040_run_log.up.sql`/`.down.sql`
- Modify: `services/services.go` (replace the unused `Sessions`/`SessionsKey` with `RunLog`/`RunLogKey`)

- [ ] Tables:
  - `runs` (id, tenant, parent run, profile name, profile hash, model, config hash, status, usage, cost, started/ended);
  - `run_events` (run, seq, member, type, payload jsonb, at), append-only through a trigger that rejects UPDATE/DELETE.
- [ ] Event types:
  - `run/start`, `run/end`
  - `model/call` (model, tokens, finish reason, latency, **request hash**; prompt text only when `LogPrompts` is on)
  - `tool/call`, `tool/result` (name, args, result hash + compact result, evidence)
  - `structured/submit`, `reduce/decision` (the winning rule)
  - `review/*` (Task 7)
- [ ] A sink adapter (`runlog.Sink(store, runID, member)`) attaches to the team and loop, with no change to the loop beyond the events it already emits. Tool, model and structured events are emitted through the scope.
- [ ] `replay.Project(events) Projection` rebuilds the outcome from the log.
- [ ] Tests:
  - one team run with fakes → contiguous `seq`, and the projection is identical to the in-memory `team.Result`;
  - UPDATE on `run_events` is rejected by the database (repository integration test, following the existing integration tests);
  - tenant A cannot read tenant B's runs.
- [ ] Commit: `feat(harness): add an append-only run event log`.

### Task 6: Evidence model

**Files:**
- Create: `server/internal/harness/evidence/evidence.go`, `evidence_test.go`
- Modify: `services/services.go` (`ToolResult.Evidence []evidence.Item`; `Reference` stays, mapped to `Kind:"DOCUMENT"`)

```go
type Item struct {
    Kind    string            `json:"kind"`    // DOCUMENT | RULE | GRAPH | SERVICE | RECORD | ... (registered)
    Ref     string            `json:"ref"`     // stable id: chunk id, rule id@version, edge id, record#field
    Excerpt string            `json:"excerpt"`
    Meta    map[string]string `json:"meta,omitempty"` // e.g. version, effective date, article
}
```

- [ ] `evidence.Registry` records the valid kinds and an optional `Resolver` per kind (checks that a `Ref` exists).
- [ ] Every evidence item returned by a tool in a run is collected into `Outcome.Evidence`.
- [ ] `evidence.Check(structured json.RawMessage, path string, seen []Item)` verifies that every evidence `ref` in the structured result really appeared in a tool result **in the same run**. This stops the model from inventing citations.
- [ ] Test:
  - a structured result citing a ref never returned by a tool → violation `EVIDENCE_UNGROUNDED`, fed back as a structured repair (Task 2);
  - KB `Reference` → `Item{Kind:"DOCUMENT"}` with no data lost.
- [ ] Commit: `feat(harness): add grounded evidence items`.

### Task 7: Human review inbox

DSH counterparts: `interaction/user-questions`, `user-approval`.

**Files:**
- Create: `server/internal/harness/review/review.go` (interface + memory), tests
- Create: `server/internal/repositories/review.go`, `migrations/000041_review_inbox.up.sql`
- Create: `server/internal/handlers/review.go` (generic REST: list, get, decide), tests
- Modify: `harness/mount.go` (an optional `ApprovalAnswer` backed by the inbox)

- [ ] `review.Item`: run id, subject (an opaque id from the consumer), the proposed result (structured JSON), evidence, reason for review, required role, status (`OPEN`/`DECIDED`/`EXPIRED`).
- [ ] `review.Decision`:
  - reviewer, role, final result (structured JSON, validated against the same schema), per-field override, reason, time;
  - history is kept, and the latest decision is the one that counts.
- [ ] `review.Policy` is a reducer (Task 4) that decides which results go into the inbox, plus a configurable sample rate for results that "need no review", so false negatives can be measured.
- [ ] **Async approvals:** a tool `ask` creates an inbox item. With `ApprovalMode: "wait"` the run waits (bounded by `ApprovalTimeout`, then `APPROVAL_UNAVAILABLE`). This wires the existing `ApprovalAnswer` to a real source.
- [ ] Every decision is written to the run log (`review/decided`).
- [ ] Tests:
  - a role without permission → 403;
  - a decision that fails the schema → 400;
  - approval wait timeout → `APPROVAL_UNAVAILABLE`, and the tool never runs;
  - decision history is kept.
- [ ] Commit: `feat(harness): add a human review inbox` and `feat(harness): back tool approvals with the review inbox`.

**Checkpoint B:** a team run is recorded in full in the log; uncertain results go to the inbox; a person decides; the decision is recorded and the projection shows the final result.

---

## Phase 3 — Deterministic knowledge and external services

### Task 8: Declarative rule engine

**Files:**
- Create: `server/internal/harness/rules/{expr,engine,load}.go` + tests
- Create: `server/internal/harness/rules/tool.go` (generic `rule_check` tool)

- [ ] A rule is data (YAML/JSON): `id`, `version`, `group`, `severity`, `when` (expression), `message` (template), `source` (free metadata, e.g. regulation and article), `effective_from`/`effective_to`.
- [ ] Expression language:
  - pure, no I/O, no loops;
  - operators: field path, `== != < <= > >=`, `in`, `exists`, `and/or/not`, `len`, `between`, date arithmetic in days;
  - **static type-checking** against the subject's JSON schema at load time;
  - a rule with an unknown field is rejected at startup.
- [ ] `Engine.Evaluate(subject json.RawMessage, group string, at time.Time) []Hit{RuleID, Version, Fired bool, Message, Evidence}`. Each hit is an `evidence.Item{Kind:"RULE", Ref:"<id>@<version>"}` plus the fields it read.
- [ ] The engine can be called **without an LLM**, as a stand-alone baseline for experiments.
- [ ] Tests:
  - every operator;
  - an invalid rule → load error with its line;
  - effective dates (one rule in two versions, a subject date between them);
  - determinism: the same input → byte-identical output.
- [ ] Commit: `feat(harness): add a declarative versioned rule engine`.

### Task 9: External service tool

DSH counterpart: `connector/*` (narrower: HTTP JSON only).

**Files:**
- Create: `server/internal/harness/httptool/httptool.go`, tests
- Refactor: extract the pinned-dial/egress-policy transport from `lib/kbembed/transport.go` into `server/internal/lib/egress/` (shared, behavior unchanged, kbembed tests stay green)

- [ ] `httptool.New(Spec{Name, Description, Params schema, URL, Timeout, Retries, Map func(resp) (ToolResult, error)})` → `services.Tool`, read-only by default.
- [ ] Egress policy:
  - allowlisted hosts;
  - no private addresses unless explicit;
  - TLS required unless `AllowPlainHTTP` (dev);
  - response size limit;
  - the response body is never passed to the model raw; it always goes through `Map`.
- [ ] Service down → `ToolResult` with code `SERVICE_UNAVAILABLE` (not a run error), so the agent can treat the data as unavailable.
- [ ] Commit: `refactor(lib): share the egress-pinned transport` and `feat(harness): add an egress-guarded http tool`.

### Task 10: Knowledge retrieval filters

**Files:**
- Modify: `lib/kb` (ingest metadata), `repositories/knowledge*.go`, `migrations/000042_kb_metadata.up.sql`
- Modify: `handlers/knowledge_tool.go` → a generic `kb_search` tool with filters

- [ ] Document and chunk metadata: free `attributes jsonb` + `valid_from`/`valid_to` + `version`. The meaning (e.g. regulation number, article) is left to the consumer.
- [ ] `kb_search` accepts `filters` (attribute equality) and `valid_at` (date). The lexical and semantic paths (`WeightedRRF`) apply the same filters.
- [ ] Results become `evidence.Item{Kind:"DOCUMENT", Meta: attributes + version}`.
- [ ] Tests:
  - two versions of a document, `valid_at` between them → only the right version is returned;
  - a filter is applied on both paths;
  - the existing chat (no filter) returns identical results.
- [ ] Commit: `feat(kb): filter retrieval by metadata and validity period`.

### Task 11: Determinism and LLM record/replay

**Files:**
- Modify: `lib/ai/chat.go` (`ChatOptions.Temperature *float64`, `Seed *int`, sent only when set), `LoopConfig` / `Profile`
- Create: `server/internal/harness/llm/replay.go`, `replay_test.go`

- [ ] Temperature and seed per profile. Model name and version go into `runs` (Task 5).
- [ ] `llm.Record(inner, store)`, `llm.Replay(store)`:
  - the key is a hash of (model, messages, tool specs, options);
  - the value is the result + deltas + usage + error code;
  - replay of a missing key → error `REPLAY_MISS`, **never** a fallback to the provider;
  - the store is a file directory (JSONL), so a fixture can be committed or archived.
- [ ] Test: record a team run with fakes, then replay → an identical run log (excluding timestamps).
- [ ] Commit: `feat(harness): add deterministic model options and llm record-replay`.

### Task 12: Egress transforms (redaction)

**Files:**
- Create: `server/internal/harness/llm/transform.go` (LLM middleware), `server/internal/harness/redact/redact.go`, tests

- [ ] `llm.WithTransform(inner, t)`:
  - `t.Outbound(messages)` runs before the request;
  - `t.Inbound(result)` runs after the response (to restore pseudonyms in text and tool arguments, so tools receive the real values).
- [ ] `redact.Patterns`:
  - a registry of named regexes + a stable pseudonym per run (`<ID_1>`);
  - the mapping lives only in memory or in the run log, encrypted, never sent out;
  - patterns are supplied by the consumer (generic examples in tests: email, phone number).
- [ ] Test: a fake LLM's request never contains the original value; tools receive the original value; `Outcome.Content` has the original value restored.
- [ ] Commit: `feat(harness): add egress transforms and identifier redaction`.

---

## Phase 4 — Measurement

### Task 13: Cost accounting

DSH counterpart: `session-stats`.

**Files:**
- Create: `server/internal/harness/cost/cost.go`, tests
- Modify: `team`, `runlog` (cost per member and per run)

- [ ] A price table per model (input/output per 1M tokens, cache hit optional) from config, versioned.
- [ ] Cost is computed from billed usage (including failed attempts, which are already summed in `call()`).
- [ ] Test: a team of 3 members → total cost = the sum of the members; a model missing from the table → cost `null` + a warning, not a crash.
- [ ] Commit: `feat(harness): account cost per agent and run`.

### Task 14: Experiment harness

**Files:**
- Create: `server/internal/harness/experiment/{dataset,runner,metrics,report}.go` + tests
- Create: `server/cmd/experiment/main.go`
- Reuse: the encode/aggregate patterns of `lib/kbeval`

- [ ] `Dataset`: JSONL `{id, input json, label json}`.
- [ ] `Arm`: a name + `func(ctx, input) (output json, RunStats, error)`.
  - The consumer defines its arms, e.g. "rules only", "one agent", "team". The harness knows no domain.
  - Built-in adapters: `RulesArm(engine)`, `ProfileArm(profile)`, `TeamArm(members, reducer)`.
- [ ] Runner:
  - every arm on the same dataset;
  - parallelism limit;
  - replay mode (Task 11) so results can be repeated;
  - each item gets its own `run id` in the run log.
- [ ] Generic metrics:
  - classification (label field path): precision/recall/F1 macro and micro, confusion matrix;
  - latency p50/p95;
  - tokens and cost per item;
  - evidence grounding rate (Task 6);
  - human-agreement rate when labels come from the review inbox (Task 7).
- [ ] Report: JSON + Markdown tables. A sample export (CSV) for human rating of explanation quality.
- [ ] Test: a 5-item fixture, 2 arms with fakes → metrics match manual calculation, and the report is byte-identical in replay.
- [ ] Commit: `feat(harness): add an experiment runner with classification metrics`.

**Checkpoint C:** one dataset, three arms (rules / profile / team), reproducible from replay, with a report ready for a paper. All generic.

---

## Order and dependencies

```
1 schema ─► 2 structured ─► 3 profile ─► 4 team+reduce ──► [A]
                                  │
6 evidence ◄──────────────────────┘
5 runlog ─► 7 review ──────────────────────────────────────► [B]
8 rules (needs 1, 6)   9 httptool   10 kb filters (needs 6)
11 replay   12 redact   13 cost (needs 4, 5) ─► 14 experiment ► [C]
```

Tasks 8–12 can run in parallel after Phase 1. Task 14 needs 4, 5, 8 and 11.

## How the claim verification plan uses this

| Claim plan task | Uses foundation |
|---|---|
| 1–2 verdict + submit_verdict | Tasks 1, 2, 6 (the verdict schema is just a `ResultSpec`) |
| 3 six agents | Task 3 (6 embedded profiles) |
| 4 orchestrator + aggregation | Task 4 (`team.Run` + `reduce.Rules`) |
| 5 claim schema + read tools | domain only (tools return `evidence.Item{Kind:"RECORD"}`) |
| 6 rule engine | Task 8 (domain supplies only the rule catalog) |
| 7 audit | Task 5 |
| 8 regulation citations | Task 10 (domain fills `attributes`) |
| 9 review queue | Task 7 (domain supplies only the UI and roles) |
| 10 KG, tariff, anomaly | domain tools; anomaly through Task 9 |
| 11 three-arm evaluation | Task 14 (domain supplies only the dataset and arm definitions) |
| 12 reproducibility + redaction | Tasks 11, 12 (domain supplies the identifier patterns) |

## Execution status

All 14 tasks landed on `main` between `90321ee` and `7f942cd`; the 2026-11-24 review fix series follows in `FIX`.

| Task | Commit |
|---|---|
| 1 JSON schema subset | `75ef0f3`, `50e6eef`, `1bc020c` |
| 2 structured terminal + submit | `efbfb21`, `465d5af` |
| 3 profiles | `cf5973e` |
| 4 team + reducers | `651d6fa`, `45705b8`, `5d4f4b8`, `2ce2f52`, `a721134` |
| 5 run log | `19315fe` |
| 6 evidence grounding | `18ca76e` |
| 7 review inbox | `441ae07` |
| 8 declarative rule engine | `32f2aad` |
| 9 egress HTTP tool | `fd98a7c`, `5031367` |
| 10 KB retrieval filters | `e06a1a5` |
| 11 temperature/seed + record/replay | `1d8eea1` |
| 12 redact + transform | `d038532` |
| 13 cost accounting | `8152bd0` |
| 14 experiment harness | `7f942cd` |
| review fixes (wiring, metadata, replay) | `FIX` |
