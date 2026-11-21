---
title: "Port DeepSeek Harness features only when harness-go has a caller, and restore the old runner's budgets first"
author: Mahdy
date: 2026-11-21
branch: docs/dsh-core-parity-plan
cost: { minutes: 16, tokens: 131562 }
id: l-f6b37441
scope: repo
status: active
src: agent
touches:
  - docs/superpowers/plans/2026-11-21-dsh-core-followup.md
  - docs/superpowers/plans/2026-11-21-dsh-core-parity.md
  - server/internal/harness/agentloop/loop.go
  - server/internal/lib/agent/runner.go
  - server/internal/lib/ai/chat.go
---

## Decided
- The new harness loop replaced the legacy runner in production but silently stopped enforcing several of its limits. Restoring them (Phase A) comes before any new DeepSeek Harness (DSH) port.
- A DSH algorithm is ported only when harness-go code will call it. Seq-ranges, event-log repair and summary compaction are deferred because harness-go has no session event log, and summary compaction adds an LLM call that billing must account for.
- Retry routes on the DSH *default-retried* codes (EMPTY_RESPONSE, RATE_LIMIT, SERVER, TIMEOUT, TRANSPORT), not on the larger configurable `retryChoice` set.
## Tried and ruled out
- The earlier follow-up plan (port failure codes, seq-ranges, repair and tool pairing as standalone packages): 3 of 4 had no caller, and it missed live bugs. It is superseded and kept for history.
## Watch out
- `agentloop` dispatch drops calls past MaxToolCalls but keeps them in the assistant message. Providers that follow the OpenAI format then reject the request with a 400.
- The legacy LoopGuards were never wired in production (`git log -S LoopGuards` shows only the foundation commit). Restoring them is a lost capability, not a regression fix.
- `approval "never"` means "allow" in harness-go but "reject approval requests" in DSH.
- Reading the provider error body to detect context overflow or quota must stay inside `lib/ai`; pass only the error code upward.
In the code:
- server/internal/harness/agentloop/loop.go (dispatch, shouldContinue, ResolveLoopConfig fields not enforced)
- server/internal/lib/agent/runner.go:197,222,255,475 (legacy budgets)
- server/internal/lib/ai/chat.go:80 (ProviderError has no body by design)
- docs/superpowers/plans/2026-11-21-dsh-core-parity.md
