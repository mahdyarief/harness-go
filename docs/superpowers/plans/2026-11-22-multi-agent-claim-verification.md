# Multi-Agent Claim Verification Foundation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Depends on:** `2026-11-22-harness-foundation-capabilities.md`.
- Generic capabilities (schema, structured output, profiles, team, run log, evidence, review inbox, rule engine, http tool, KB filters, replay, redaction, cost, experiment) are built there.
- The tasks below keep only the domain parts. The mapping is in "How the claim verification plan uses this" at the end of the foundation plan.
- When a task here overlaps a foundation task, build on the foundation version instead of building a domain-specific one.

**Goal:** Turn harness-go from a single-agent chat harness into the runtime for the thesis *"Framework Multi-Agentic AI untuk Verifikasi dan Validasi Klaim BPJS Kesehatan dengan Pendekatan Hybrid RAG, Explainable AI, dan Human-in-the-Loop"*. It replaces DeepSeek Harness (DSH) as the agent runtime and orchestrator.

**Thesis source:** `deskripsi_framework_multi_agentic_ai_bpjs.md`, which defines:
- 6 agents: Dokumen, Klinis, Coding, Tarif, Anomali, Kepatuhan
- 3 verdicts: Sesuai / Perlu Review / Tidak Sesuai
- the knowledge sources: Hybrid RAG + knowledge graph + rule engine + ML
- HITL by a verifikator, dokter or koder
- 3 experiment arms: rule-based / LLM tunggal / multi-agent
- evaluation on P/R/F1, time, explanation quality, cost and SUS/UTAUT

**DSH reference:** `D:\Github\agent-workspace\deepseek-harness` @ `2e39e70496aa02986ca6a820c5d497e816b69d7e`. Cite the DSH package in the doc comment of every port.

**Tech Stack:**
- Go module `hsg` on Fiber v3 + PostgreSQL 18 (RLS per tenant)
- the existing harness: `server/internal/harness/*`
- the existing Hybrid RAG: `lib/kb` (`WeightedRRF`), `lib/kbembed`

**Status: NOT STARTED** (planned 2026-11-22). Tick boxes and add commit hashes to an "Execution status" section as tasks land.

---

## What already exists and what does not

| Thesis need | harness-go today | Gap |
|---|---|---|
| Single-agent runtime (tools, retry, compaction, budgets, approval) | `harness/agentloop`, which is parity with the DSH core | — |
| Hybrid RAG | `lib/kb` lexical `tsvector('indonesian')` + embedding, `WeightedRRF`, PDF/DOCX/XLSX/CSV | Citations have no regulation metadata (article, version, effective date) |
| Structured verdict output | `ai.ChatOptions` has only `Tools`/`ToolChoice`/`MaxTokens`. Arguments are checked only for keys (`agentloop.validateArgs`) | No full schema validation, no verdict contract |
| Multi-agent orchestration | none (DSH: `subagent/subagent-spawn-in-process`, `team/agent-team`) | Everything |
| Rule engine, knowledge graph, ML anomaly | none | Everything |
| Audit trail per claim | general audit `repositories/audit.go` (actor/action/metadata) | No step log for agent, tool, evidence or model |
| HITL | approval only gates *tools*, and `ApprovalAnswer` is not wired anywhere | No claim review queue |
| Experiment evaluation | `lib/kbeval`/`cmd/kbeval` (retrieval only) | No claim-level evaluation for the three arms |
| Reproducibility | none | Model pinning, temperature 0, record/replay LLM |
| Patient data | `README.md`: *"does not process claims or patient data"* | Scope change, PII redaction, egress |

**Not ported from DSH:** computer-use, browser, shell, ssh, the sandbox runner, PTC and `workflow` scripts, goal rounds, MCP, summarizer compaction.
- They are features for coding agents.
- They widen the attack surface of a health-data system.
- They do not help answer the research questions.

## Global Constraints

- TDD per task: RED → GREEN → REFACTOR. Watch each new test fail first. Conventional Commit per task.
- Files ≤500 LOC; `gofmt`; `python scripts/pre_commit_check.py files`; `trail check` before commit and push.
- Read `.trail/learnings/` first, especially:
  - port DSH features only when they have a caller;
  - fake the LLM the way the real gateway fails;
  - the deliberate deviations from DSH.
- `lib/ai` stays the only egress. Provider body text is never exposed upward.
- **Decisions are made in deterministic code; the LLM explains and judges.**
  - Rule, tariff and code-grouping results come from tools.
  - A model never "computes" a tariff.
  - This is what makes the explanations auditable and the experiment repeatable.
- Every new claim table is tenant-scoped with RLS (the pattern in `migrations/000006_tenants.up.sql`). New migrations continue from `000040`.
- Model-facing text is in Indonesian. Codes are SCREAMING_SNAKE.
- **No real data in the repo.** Fixtures are synthetic. Anonymized data from the hospital or BPJS lives outside git.

## Decisions to confirm before execution

**D1 — Structured output through a forced tool call, not `response_format`.**
- Each agent ends its run by calling `submit_verdict`, whose JSON schema is the verdict contract.
- Reasons:
  - DeepSeek supports `json_object`, not strict `json_schema`.
  - `chatBody` (`lib/ai/chat.go:194`) already normalizes `ToolChoice` to the named-function form.
  - `LoopConfig.ForceToolCall` already exists.
  - Validation happens on our side, so it is provider-independent.
- Alternative: add `ResponseFormat` to `ai.ChatOptions`. That works only for providers with strict schema support.

**D2 — Deterministic orchestrator, not model-driven delegation.**
- The claim pipeline is fixed in code: preprocess → 6 agents in parallel → aggregate → review queue.
- DSH `workflow`/`tool-subagent` lets the model decide when to delegate. That makes experiment runs non-deterministic and hard to compare across arms.
- The orchestrator borrows from DSH:
  - isolated children (`subagent-spawn-in-process`: one scope per agent, separate history and budget);
  - a per-child status view.

**D3 — Aggregation is a code rule, not an LLM.**
- Status is computed in a fixed order:
  1. If any agent says `Tidak Sesuai` with a finding backed by a rule or tariff tool → `Tidak Sesuai`.
  2. Else, if any agent says `Perlu Review`, or confidence is below a threshold, or agents conflict → `Perlu Review`.
  3. Otherwise → `Sesuai`.
- The thresholds are configuration and are reported in the thesis.
- An optional "summarizer" agent only writes the narrative and never changes the status.

**D4 — Knowledge graph in Postgres (relation tables + recursive CTEs), not a graph database.**
- One fewer deployment component, and RLS stays consistent.
- Neo4j can be added later if a multi-hop query proves necessary.

**D5 — The ML anomaly model runs as a separate HTTP service (Python), called through one tool.**
- The Go side only defines the contract: features in → `score`, `top_features` out.
- Training stays in the research notebook.

**D6 — Thesis title wording.**
- Replace "Berbasis DeepSeek Harness" with "berbasis agent harness open-source (harness-go) yang mengadopsi arsitektur DeepSeek Harness".
- Check license compatibility: MIT from govi-starter-kit, and the DSH license if any code or text is adapted.

---

## Phase 1 — Multi-agent runtime (makes the thesis runnable)

### Task 1: Verdict contract and strict schema validation

**Files:**
- Create: `server/internal/harness/verdict/verdict.go`, `verdict_test.go`
- Create: `server/internal/harness/agentloop/schema.go` (replaces `validateArgs`), `schema_test.go`

**Contract:**
```go
type Status string // "SESUAI" | "PERLU_REVIEW" | "TIDAK_SESUAI"

type Evidence struct {
    Kind    string `json:"kind"`    // "RULE" | "DOCUMENT" | "REGULATION" | "KG" | "ML" | "TARIFF"
    Ref     string `json:"ref"`     // rule id, chunk id, kg edge id, ...
    Excerpt string `json:"excerpt"`
}
type Finding struct {
    Aspect   string     `json:"aspect"`   // ADMINISTRATIF|KLINIS|CODING|TARIF|ANOMALI|KEPATUHAN
    Severity string     `json:"severity"` // INFO|MINOR|MAJOR
    Message  string     `json:"message"`
    Evidence []Evidence `json:"evidence"`
}
type Verdict struct {
    Agent      string    `json:"agent"`
    Status     Status    `json:"status"`
    Confidence float64   `json:"confidence"` // 0..1
    Findings   []Finding `json:"findings"`
    Rationale  string    `json:"rationale"`
}
```

- [ ] Test (RED): a `Verdict` with an unknown status, confidence >1, or a `MAJOR` finding with no evidence → `Validate()` returns an error with code `VERDICT_INVALID`.
- [ ] `Schema()` returns the JSON Schema used for the `submit_verdict` tool parameters (enum, required, `additionalProperties:false`).
- [ ] Replace `validateArgs` with a validator that checks `type` / `enum` / `required` / `minimum` / `maximum` / nested `items` / `properties`. Use a small hand-written subset, enough for this schema, so no new dependency is needed. The existing tests in `agentloop` stay green.
- [ ] Commit: `feat(harness): add verdict contract and schema validation`.

### Task 2: `submit_verdict` tool and run termination

**Files:**
- Create: `server/internal/harness/verdict/tool.go`, `tool_test.go`
- Modify: `server/internal/harness/agentloop/loop.go` (a terminal tool), `services/services.go`

- [ ] Add `services.Terminal` (optional interface): `Terminal() bool`. When a terminal tool succeeds, the loop stops without another model turn and puts its output into `Outcome.Structured json.RawMessage`.
- [ ] Test (RED): a fake LLM that calls `submit_verdict` with valid JSON → `Outcome.Structured` is filled, `Turns == 1`, and the LLM is not called again.
- [ ] Test: invalid JSON → the tool result is a validation error message (Indonesian) and the model gets another turn. After `MaxVerdictRepairs` (default 2), the run ends with `VERDICT_INVALID` and is treated as `PERLU_REVIEW` by the orchestrator (Task 4).
- [ ] Test: the model replies in text without calling the tool → one nudge, "Akhiri dengan memanggil submit_verdict", then `VERDICT_MISSING`.
- [ ] Commit: `feat(harness): end agent runs with a validated submit_verdict call`.

### Task 3: Agent definitions

**Files:**
- Create: `server/internal/claimcheck/agents/spec.go`, `spec_test.go`
- Create: `server/internal/claimcheck/agents/prompts/{dokumen,klinis,coding,tarif,anomali,kepatuhan}.md` (embedded with `//go:embed`)

```go
type Spec struct {
    Name       string   // "dokumen", "klinis", ...
    Aspect     string   // ADMINISTRATIF, KLINIS, ...
    System     string   // embedded prompt
    Tools      []string // allowlist → services.ToolAllowlistKey
    LoopConfig services.LoopConfig
    PromptHash string   // sha256(System), logged in audit (Task 7)
}
```

- [ ] Test: every spec has a unique name, its tools exist in the registry, `submit_verdict` is always in the allowlist, and `PromptHash` is stable.
- [ ] Each agent's tool allowlist (an agent cannot reach another agent's tools):
  - Dokumen: `claim_documents`, `rule_check`
  - Klinis: `claim_clinical`, `kb_search`, `kg_relations`
  - Coding: `claim_codes`, `kg_relations`, `rule_check`
  - Tarif: `claim_billing`, `tariff_lookup`, `rule_check`
  - Anomali: `claim_billing`, `anomaly_score`
  - Kepatuhan: `kb_search`, `rule_check`
- [ ] Commit: `feat(claimcheck): define the six verification agents`.

### Task 4: Deterministic orchestrator

DSH counterparts: `subagent-spawn-in-process` and `team/agent-team`.

**Files:**
- Create: `server/internal/harness/team/team.go`, `team_test.go` (generic, knows nothing about claims)
- Create: `server/internal/claimcheck/orchestrator.go`, `orchestrator_test.go`

- [ ] `team.Run(ctx, members []Member, input, sink)`:
  - one `harness.Mount` (its own scope) per member;
  - runs in parallel with a `MaxAgents` limit;
  - a per-member deadline;
  - one member failing does not cancel the others;
  - results are returned in input order (deterministic).
- [ ] Each member's events go to the sink with an `Agent` field. Add the `Agent string` field to `services.AIEvent` **only** with `omitempty`, so the chat UI SSE stays legacy.
- [ ] Test (RED): 3 fake members, one hangs past its deadline → the other 2 results still come back, and the hung one gets status `PERLU_REVIEW` with error code `AGENT_TIMEOUT`.
- [ ] Test: per-member usage is summed into a `TeamOutcome` (tokens, duration per agent and total).
- [ ] `claimcheck.Verify(ctx, claim) (Report, error)`: preprocess (Task 5 tools load the claim) → `team.Run` with the 6 specs → `aggregate` (D3).
- [ ] Test the aggregation table:
  - every combination of SESUAI / PERLU_REVIEW / TIDAK_SESUAI;
  - low confidence;
  - a TIDAK_SESUAI without RULE/TARIFF evidence → PERLU_REVIEW.
- [ ] Commit: `feat(harness): run agent teams with isolated scopes` and `feat(claimcheck): orchestrate six agents and aggregate verdicts`.

### Task 5: Claim data model and claim reading tools

**Files:**
- Create: `migrations/000040_claims.up.sql`/`.down.sql`
  - tables: `claims`, `claim_diagnoses`, `claim_procedures`, `claim_billing_items`, `claim_documents`
  - all with `tenant_id` + RLS
- Create: `server/internal/models/claim.go`, `server/internal/repositories/claim.go`
- Create: `server/internal/claimcheck/tools/claim_read.go` (`claim_documents`, `claim_clinical`, `claim_codes`, `claim_billing`), plus tests
- Create: `server/internal/claimcheck/testdata/claims/*.json` (synthetic claims)

- [ ] Fields follow the thesis input:
  - SEP (number, date, class, type of care)
  - medical summary
  - diagnoses with ICD-10 (primary/secondary)
  - procedures with ICD-9-CM
  - supporting results
  - billing items
  - INA-CBG data (group code, severity, tariff)
- [ ] The tools are read-only and return compact JSON. Every value carries an `Evidence{Kind:"DOCUMENT", Ref:"claim:<id>#<field>"}` locator, so a finding can point at the exact field.
- [ ] Test the RLS: tenant A cannot read tenant B's claims (follow the existing tenant tests).
- [ ] Commit: `feat(claimcheck): add claim schema and read-only claim tools`.

### Task 6: Rule engine

**Files:**
- Create: `server/internal/claimcheck/rules/engine.go`, `rules.go`, `engine_test.go`
- Create: `server/internal/claimcheck/rules/catalog/*.yaml` (versioned rules)
- Create: `server/internal/claimcheck/tools/rule_check.go`

- [ ] Rules are **data** (YAML), not Go code: `id`, `version`, `aspect`, `severity`, `when` (a predicate expression over claim fields), `message`, `source` (regulation + article).
- [ ] The predicate evaluator is small and pure: field comparisons, `in`, `exists`, `and/or/not`, numeric ranges. No `eval` and no scripting language.
- [ ] Test (RED): a synthetic claim with an LOS that does not fit the type of care → rule `ADM-LOS-001` fires, with an `Evidence{Kind:"RULE", Ref:"ADM-LOS-001@v1"}` pointing at the claim field.
- [ ] `rule_check(aspect)` returns the rules that fired and those that passed. The engine is also callable without an LLM: it is **experiment arm 1 (rule-based)**.
- [ ] Start with ~10 rules from public guidelines. The full catalog is research work (see "Domain data").
- [ ] Commit: `feat(claimcheck): add a declarative versioned rule engine`.

---

## Phase 2 — Explainability, audit, HITL

### Task 7: Audit log per claim (append-only)

DSH counterpart: `session/session-persistence-*` (event log).

**Files:**
- Create: `migrations/000041_claim_runs.up.sql`
  - `claim_runs` (run id, claim, arm, model, model version, config hash, status, cost, duration)
  - `claim_run_events` (seq, run, agent, type, payload jsonb, created_at); append-only through a trigger that rejects UPDATE/DELETE
- Create: `server/internal/claimcheck/audit/recorder.go`, `recorder_test.go`

- [ ] The recorder is a sink that writes, per event:
  - agent start/end with `PromptHash`
  - each LLM call (model, tokens, finish reason, latency; **not** the full prompt by default, see Task 12)
  - each tool call (name, args, result hash + compact result, references)
  - each verdict
  - the aggregation result with the D3 rule that decided it
- [ ] Test: one `Verify` with fakes → the events have contiguous `seq` and can be reconstructed into a report identical to `Report`. This is the **audit replay** guarantee.
- [ ] Test: UPDATE on `claim_run_events` is rejected by the database.
- [ ] Commit: `feat(claimcheck): record an append-only audit trail per claim run`.

### Task 8: Regulation citations in the RAG

**Files:**
- Modify: `lib/kb` (ingest metadata), `migrations/000042_kb_regulation_meta.up.sql`, `services.Reference`
- Modify: `handlers/knowledge_tool.go` (or a new `kb_search` tool for claimcheck)

- [ ] Add optional metadata per document and chunk: `regulation_id` (e.g. "Permenkes 3/2023"), `article` (Pasal/ayat), `version`, `effective_from`, `effective_to`.
- [ ] `services.Reference` gets the same fields with `omitempty`, so the chat UI is unaffected.
- [ ] The `kb_search` tool can filter `effective_at = claim discharge date`, so a claim is judged by the rules in force when it happened.
- [ ] Test: two versions of the same regulation, a claim dated between them → only the right version is returned.
- [ ] Commit: `feat(kb): attach regulation metadata to knowledge chunks`.

### Task 9: Human review queue

DSH counterpart: `interaction/user-questions`. This is a queue, not pausing an agent.

**Files:**
- Create: `migrations/000043_claim_reviews.up.sql` (`claim_reviews`: run, reviewer, role verifikator/dokter/koder, final decision, per-finding override, reason, timestamp)
- Create: `server/internal/handlers/claim_review.go`, `repositories/claim_review.go`, tests
- Create (web): a queue page + review detail (findings, evidence linked to documents/regulations/rules, accept/reject per finding)

- [ ] `PERLU_REVIEW` and `TIDAK_SESUAI` go into the queue. A sample of `SESUAI` is also queued (a configurable percentage) to measure false negatives.
- [ ] The final decision **always** belongs to a human. The system status is a recommendation (thesis section 4.6). The API prevents a claim from being "final" without a review.
- [ ] The human decision and per-finding overrides are saved and become **ground truth labels** for the evaluation (Task 11).
- [ ] Test: a review by a role without permission → 403. A double review → the latest is recorded and the history is kept.
- [ ] Commit: `feat(claimcheck): add the human review queue`.

---

## Phase 3 — Knowledge, anomaly, experiment

### Task 10: Knowledge graph and anomaly tools

**Files:**
- Create: `migrations/000044_claim_kg.up.sql`
  - `kg_nodes` (type: ICD10 | ICD9CM | INACBG | TARIF | RULE)
  - `kg_edges` (from, to, relation e.g. `DIAGNOSIS_SUPPORTS_PROCEDURE`, `GROUPS_INTO`, `PRICED_AT`, `CONSTRAINED_BY`, source, version)
- Create: `server/internal/claimcheck/tools/kg_relations.go`, `tariff_lookup.go`, `anomaly_score.go`, tests
- Create: `server/cmd/kgload/` (CSV loader for reference data)

- [ ] `kg_relations(code, relation, depth≤2)` → edges + `Evidence{Kind:"KG", Ref:"edge:<id>"}`, using a recursive CTE.
- [ ] `tariff_lookup(inacbg, class, regional)` → the tariff from reference data, with `Evidence{Kind:"TARIFF"}`.
- [ ] `anomaly_score(claim)`:
  - HTTP POST to the ML service (URL from config);
  - the timeout, retry and egress rules follow `lib/kbembed` (pinned dial, no private address unless configured);
  - when the service is down → a `ML_UNAVAILABLE` result, so the Anomali agent can say `PERLU_REVIEW` and does not crash.
- [ ] Commit per tool.

### Task 11: Three-arm evaluation harness

**Files:**
- Create: `server/cmd/claimeval/main.go`
- Create: `server/internal/claimcheck/eval/{dataset,arms,metrics,report}.go` + tests
- Reuse: the `lib/kbeval` patterns (aggregate, report encoding)

- [ ] Dataset: JSONL of `{claim, label_status, label_findings_per_aspect}`, from the anonymized data (outside git) or from human reviews (Task 9).
- [ ] The three arms run against **the same dataset and the same snapshot of reference data**:
  1. `rule` — rule engine only (Task 6) + D3 aggregation, no LLM.
  2. `single` — one LLM agent with every tool and one combined prompt, ending with `submit_verdict`.
  3. `multi` — the Task 4 orchestrator.
- [ ] Metrics:
  - precision/recall/F1 per status and per aspect (macro + micro)
  - confusion matrix
  - latency p50/p95 per claim
  - tokens + cost per claim (price per model from config)
  - evidence coverage: share of findings with ≥1 valid evidence item
  - evidence validity: the referenced rule, chunk or edge really exists and matches
- [ ] Explanation quality: export a sample for human rating on a rubric (CSV). Optionally add an LLM judge, reported separately and never used as the primary metric.
- [ ] Output: JSON + Markdown tables ready for the results chapter.
- [ ] Test: a 5-claim fixture with fake LLMs → deterministic metrics that match manual calculation.
- [ ] Commit: `feat(claimcheck): add three-arm claim evaluation`.

### Task 12: Reproducibility and patient data

**Files:**
- Modify: `lib/ai` (temperature/seed in `ChatOptions`, sent when set), `harness/llm`
- Create: `server/internal/harness/llm/replay.go` (record/replay LLM adapter), tests
- Create: `server/internal/claimcheck/redact/redact.go`, tests
- Modify: `README.md` (system scope)

- [ ] `ChatOptions.Temperature *float64`, `Seed *int`. The evaluation sets `0` and a fixed seed. Model name and version go into `claim_runs`.
- [ ] `replay` LLM:
  - record mode saves a request hash → response (including tool calls and usage) to a file;
  - replay mode answers from the file, and a missing hash → an error, never a silent call to the provider.
  - This lets the thesis experiment be rerun without new API cost, with results the examiner can verify.
- [ ] PII redaction before egress:
  - names, NIK, BPJS card numbers, addresses and phone numbers are replaced with stable pseudonyms per claim;
  - the mapping is never sent out.
  - Test: no NIK or card-number pattern reaches the fake LLM's request.
- [ ] Optional on-prem endpoint: the gateway already takes `base_url`. Document how to use an open-weights DeepSeek model through an OpenAI-compatible server.
- [ ] Update the `README.md` scope sentence ("does not process claims or patient data") to match: anonymized data only, redaction on, and the audit log does not store full prompts by default.
- [ ] Commit: `feat(harness): add deterministic options and llm record-replay` and `feat(claimcheck): redact identifiers before model calls`.

---

## Domain data (research work, not code)

These items gate the quality of the results. The code above only provides the containers.

| Data | Used by | Source (to be confirmed with supervisor/partner) |
|---|---|---|
| Rule catalog (administrative, coding, tariff, compliance) | Task 6 | BPJS/Kemenkes verification guidelines, INA-CBG guidance |
| ICD-10, ICD-9-CM, INA-CBG grouping, tariff tables | Task 10 | Official reference data; check redistribution licenses |
| Regulations and clinical guidelines | Task 8 | Public regulations; PNPK |
| Labeled anonymized claim dataset | Task 11 | Hospital/BPJS partner; ethics clearance and data agreement |
| Anomaly model + features | Task 10 | Research notebook; trained on the training split only |

## Suggested order and checkpoints

1. Tasks 1–2 → **checkpoint A:** one agent produces a valid verdict against a synthetic claim.
2. Tasks 3–6 → **checkpoint B:** the full multi-agent run on synthetic data, plus arm 1 (rule-based) can run.
3. Tasks 7–9 → **checkpoint C:** demo prototype (DSRM stage 4). Results are auditable and reviewable.
4. Tasks 10–12 → **checkpoint D:** the three-arm experiment can be rerun (DSRM stage 5).

Deferred (no caller yet): DSH summarizer compaction, goal rounds, model-driven `workflow` scripts, MCP, the OTel exporter. Add them only when an experiment or a deployment needs them.
