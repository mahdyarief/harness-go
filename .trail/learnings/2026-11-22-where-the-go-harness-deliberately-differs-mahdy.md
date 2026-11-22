---
title: "Where the Go harness deliberately differs from DeepSeek Harness, and why"
author: Mahdy
date: 2026-11-22
branch: fix/harness-parity-gaps
cost: { minutes: 19, tokens: 46074 }
id: l-830f68cf
scope: repo
status: active
src: agent
touches:
  - server/internal/harness/agentloop/empty_response_test.go
  - server/internal/harness/agentloop/loop.go
  - server/internal/harness/agentloop/compact.go
  - server/internal/harness/agentloop/compact_internal_test.go
  - server/internal/harness/agentloop/dispatch.go
  - server/internal/harness/agentloop/hung_tool_test.go
  - server/internal/harness/agentloop/budget_test.go
  - server/internal/harness/services/services.go
---

## Decided
- A tool that ignores its context is abandoned at its deadline, not waited for. DSH drains running tools on cancel, but Go cannot stop a goroutine, and waiting let one hung tool hold the run past ToolTimeout and MaxDuration. The late result goes to a buffered channel nobody reads.
- An empty model response (no text, no tool calls) is retried as EMPTY_RESPONSE, as DSH does. When retries run out, the run does not fail as DSH's would: the empty result goes to the continuation policy, keeping the legacy nudge-after-tools behaviour.
- Compaction may cut inside a single user turn, as DSH selectCompactableRange does. Because harness-go has no summarizer yet, the user message that opened the turn is always kept; otherwise the model would lose the task it is working on.
## Tried and ruled out
- Cutting compaction only at user messages. One question with a long tool chain then could not shrink until the provider rejected it for context overflow.
## Watch out
- An abandoned tool goroutine keeps running and has already released its concurrency slot. A write tool that ignores context can therefore still finish its side effect after the model was told it timed out.
- Fake LLMs that return empty results now pay one retry backoff (~500ms) per empty response in tests.
In the code:
- server/internal/harness/agentloop/dispatch.go (runBounded)
- server/internal/harness/agentloop/loop.go (call: empty-response retry)
- server/internal/harness/agentloop/compact.go (compactContext drop phase, keptFrom)
