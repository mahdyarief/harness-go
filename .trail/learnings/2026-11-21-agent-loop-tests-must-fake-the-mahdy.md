---
title: Agent loop tests must fake the LLM the way the real gateway fails
author: Mahdy
date: 2026-11-21
branch: fix/harness-parity-review
cost: { minutes: 17, tokens: 256231 }
id: l-77ee7c80
scope: repo
status: active
src: agent
touches:
  - server/internal/harness/agentloop/loop.go
  - server/internal/harness/agentloop/loophuard_test.go
  - server/internal/harness/agentloop/stream_capture_test.go
  - server/internal/harness/llm/adapter.go
  - server/internal/harness/llm/adapter_internal_test.go
  - server/internal/harness/llm/failure.go
  - server/internal/lib/ai/classify.go
  - server/internal/harness/agentloop/budget_test.go
  - server/internal/harness/agentloop/compact.go
  - server/internal/harness/agentloop/compact_internal_test.go
---

## Decided
- On error, the real gateway returns usage only (`usageOnly()`): no Content and no ToolCalls. A truncated stream is `ErrOutput`, not `ErrProvider`. The loop therefore captures streamed deltas itself, and treats both errors as resumable cuts.
- Token budget counts completion tokens only. The prompt is re-sent every turn and compaction already bounds it.
- The harness Runner converts the Outcome before returning the error, because the chat handler bills and persists partial answers from it.
## Tried and ruled out
- Fake LLMs that return `ChatResult{Content: ...}` together with an error. They made stream resume look tested while it never ran in production. A fake must deliver text through `onDelta` and return only token counts on error.
## Watch out
- `git stash pop` and checkout with `core.autocrlf=true` write CRLF, and the pre-commit gofmt check then fails. Run `gofmt -w` on touched files before committing.
- `lib/ai` `TestBodyDeadline` is timing-sensitive and can fail once under a full parallel `go test ./...`. Rerun it alone before blaming a change.
In the code:
- server/internal/harness/agentloop/loop.go `call`, resume `cut` condition, `remaining`
- server/internal/harness/mount.go `Runner.Run`
- server/internal/lib/ai/chat.go `usageOnly`, `readEventStream`
- server/internal/harness/agentloop/stream_capture_test.go `gatewayCutLLM`
