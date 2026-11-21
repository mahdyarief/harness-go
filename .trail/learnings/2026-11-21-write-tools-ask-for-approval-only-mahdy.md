---
title: "Write tools ask for approval only when the sandbox is read-only, as in DeepSeek Harness"
author: Mahdy
date: 2026-11-21
branch: fix/harness-parity-review
cost: { minutes: 1, tokens: 1883 }
id: l-edc1207f
scope: repo
status: active
src: agent
touches:
  - server/internal/harness/tools/pipeline.go
  - server/internal/harness/services/services.go
  - server/internal/harness/agentloop/dispatch.go
  - server/internal/harness/mount.go
---

## Decided
- Approval is an escalation step, not a gate on every write. A write tool in a workspace-write sandbox runs without asking. Only a write tool in a read-only sandbox asks.
- Approval policy "never" means no escalation is possible, so the call is denied as SANDBOX_READ_ONLY. Under "ask", the answerer comes from the scope; with no answerer the call fails closed with APPROVAL_UNAVAILABLE, and a no from the user gives APPROVAL_REJECTED. This mirrors DSH sandbox escalation plus user-approval (no answerer means unavailable).
- Pipeline checks run in a prepare step before the loop announces tool_start. A denied call is never shown as started, and the approval answerer is asked exactly once per call.
## Tried and ruled out
- Having every write tool ask, with "never" rejecting. That made the workspace preset unable to run any write tool, and approval answers could not be wired in from anywhere.
## Watch out
- Read-only tools skip the sandbox and approval checks entirely. A tool that writes must report ReadOnly() == false, or it bypasses approval.
- Calls denied by the pipeline do not count toward out.ToolCalls or the tool budget. Repeat detection and MaxTurns still bound them.
In the code:
- server/internal/harness/tools/pipeline.go (pre, sandbox, approve, Prepare)
- server/internal/harness/services/services.go (Preparer, ApprovalAnswer, ApprovalAnswerKey)
- server/internal/harness/agentloop/dispatch.go (Prepare before tool_start)
- server/internal/harness/mount.go (MountConfig.ApprovalAnswer)
