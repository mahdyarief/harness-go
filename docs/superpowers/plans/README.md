# Plans and improvement journal

Each plan uses `- [ ]` / `- [x]` boxes. A finished plan has an
"Execution status" section mapping its tasks to commits. Newest first.

| Date | Plan | Status | Commits |
|---|---|---|---|
| 2026-11-22 | [Multi-agent claim verification](2026-11-22-multi-agent-claim-verification.md): domain plan for the BPJS claim verification thesis | Not started; depends on the foundation plan | `e7db1c2` (plan) |
| 2026-11-22 | [Harness foundation capabilities](2026-11-22-harness-foundation-capabilities.md): 14 general capabilities (structured results, agent teams, run log, evidence, review inbox, rule engine, replay, experiment runner, ...) | Not started | `e7db1c2` (plan) |
| 2026-11-21 | [DSH core parity](2026-11-21-dsh-core-parity.md): make the agent loop behave 1:1 with the DeepSeek Harness core | **Done**, Phase C deferred into the foundation plan | `06b34cb..56bf337`, review fixes `be8d899..7b33c4c` |
| 2026-11-21 | [DSH core follow-up](2026-11-21-dsh-core-followup.md) | Superseded by DSH core parity | `57b3611` (plan) |
| 2026-11-21 | [harness-go](2026-11-21-harness-go.md): scope, services, pipeline and loop skeleton | **Done** | `a128621`, `d8200c0` |
| 2026-11-21 | [Pre-commit quality gate](2026-11-21-precommit-quality-gate.md): gofmt, 500 LOC, conventional commits | **Done** | `6ecbe78..1da7a7e` |

## Journal

- **2026-11-21 Foundation.** harness-go ported from govi-starter-kit and given a scope/services harness (`a128621`, `d8200c0`) behind a pre-commit gate.
- **2026-11-21 Parity plan.** The follow-up plan was reviewed and replaced: three of its four ports had no caller, and it missed live regressions (G1–G11) in the loop that replaced `lib/agent.Runner`.
- **2026-11-21 Parity executed.** Tasks 0–11: tool-call pairing, budgets, concurrency, continuation and resume, loop guards, the tool pipeline, failure codes, retry, retention and pruning, overflow recovery, estimator.
- **2026-11-21 Review fixes.** Outcome lost on error; compaction drop step was a no-op; prompt tokens wrongly budgeted; local rejections retried; stream resume unreachable in production; guards recounting old failures. Classifiers, pruner defaults, usage-baseline compaction and DSH approval semantics completed.
- **2026-11-22 Remaining gaps.** Silent turn-budget exhaustion, tools holding the run past their deadline, compaction unable to cut inside a turn, `EMPTY_RESPONSE` never produced. The deliberate differences from DSH are recorded in `.trail/learnings/`.
- **2026-11-22 Next.** General foundation capabilities first, then the claim verification thesis as their first consumer. README documents implemented vs planned capability.
