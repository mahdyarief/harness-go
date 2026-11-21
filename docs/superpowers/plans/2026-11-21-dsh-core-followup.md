# DSH Core 1:1 Follow-up Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port four more pure, Node-free DSH core algorithms into harness-go as 1:1 Go equivalents with TDD coverage.

**Architecture:** Each algorithm lives in its own package under `server/internal/harness/` following the existing layout. All are pure folds/classifiers with no Cordis/Node dependency. They reuse `services.HarnessError` (already ported in `78c0bd7`). No existing behavior is changed; each task adds new files plus focused tests.

**Tech Stack:** Go 1.x stdlib, package `hsg/internal/harness/...`, `go test`, `gofmt`, `go vet`.

## Global Constraints

- TDD per task: RED (watch fail) → GREEN (minimal code) → REFACTOR.
- Every new exported function has a test; watch each fail before implementing.
- `go test ./...` must stay green after each task.
- Never parse a `HarnessError.Message` to route — route only on `Code`.
- Model-visible text must be logged (already enforced in the timeout branch).
- Files must stay ≤500 LOC (pre-commit gate); run `gofmt`.
- Commit per task with Conventional Commits; run `trail check` before commit.
- Source of truth: `D:\Github\agent-workspace\deepseek-harness` @ `2e39e704`.

---

### Task 1: LLM failure codes + classifier

**Files:**
- Create: `server/internal/harness/llm/failure.go`
- Test: `server/internal/harness/llm/failure_test.go`
- Reference: `deepseek-harness/packages/llm/llm/src/error.ts`, `adapter-failure.ts`

**Interfaces:**
- Consumes: `services.HarnessError` (`server/internal/harness/services/services.go`)
- Produces:
  - `type FailureCode string` + `const` for each builtin code in DSH `LLM_FAILURE_CODES`.
  - `RetryableCodes() map[FailureCode]bool` for the 9 `retryChoice` entries: `EMPTY_RESPONSE`, `MALFORMED_RESPONSE`, `QUOTA`, `RATE_LIMIT`, `SERVER`, `STREAM_CLOSED`, `TIMEOUT`, `TRANSPORT` (8) — plus no others. Verify count against the TS list; the 8 above are the exact `retryChoice: true` entries.
  - `func NewFailure(message string, code FailureCode, cause error) *services.HarnessError` — wraps via `services.NewHarnessError(message, string(code), cause)`.
  - `func NormalizeFailure(err error) LlmFailure` where `LlmFailure{Message, Code string}`.
  - `func IsContextWindowExceeded(detail string) bool`
  - `func IsQuotaExceeded(detail string) bool`

**Step 1: Write the failing test**

```go
package llm_test

import (
    "errors"
    "testing"

    "hsg/internal/harness/llm"
    "hsg/internal/harness/services"
)

func TestNewFailureCarriesStableCode(t *testing.T) {
    cause := errors.New("root")
    err := llm.NewFailure("rate limited", llm.CodeRateLimit, cause)
    if got := services.ErrorCode(err); got != string(llm.CodeRateLimit) {
        t.Fatalf("ErrorCode=%q", got)
    }
    if !errors.Is(err, cause) {
        t.Fatal("cause not chained")
    }
}

func TestNormalizeFailureFromHarnessError(t *testing.T) {
    f := llm.NormalizeFailure(llm.NewFailure("kuota habis", llm.CodeQuota, nil))
    if f.Message != "kuota habis" || f.Code != string(llm.CodeQuota) {
        t.Fatalf("failure=%+v", f)
    }
}

func TestNormalizeFailurePlainErrorIsUnknown(t *testing.T) {
    f := llm.NormalizeFailure(errors.New("boom"))
    if f.Message != "boom" || f.Code != string(llm.CodeUnknown) {
        t.Fatalf("failure=%+v", f)
    }
}

func TestContextWindowClassifier(t *testing.T) {
    cases := []string{
        "context length exceeded",
        "maximum context window",
        "prompt is too long for the model",
        "input exceeds the model's context window",
    }
    for _, detail := range cases {
        if !llm.IsContextWindowExceeded(detail) {
            t.Errorf("not classified as context overflow: %q", detail)
        }
    }
    if llm.IsContextWindowExceeded("request timeout") {
        t.Error("false positive on timeout")
    }
}

func TestQuotaClassifier(t *testing.T) {
    cases := []string{
        "insufficient quota",
        "quota exceeded",
        "out of credits",
        "balance depleted",
    }
    for _, detail := range cases {
        if !llm.IsQuotaExceeded(detail) {
            t.Errorf("not classified as quota: %q", detail)
        }
    }
    if llm.IsQuotaExceeded("rate limit reached") {
        t.Error("rate limit is not terminal quota")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `cd /d/Github/agent-workspace/harness-go/server && go test ./internal/harness/llm/ -run 'TestNewFailure|TestNormalizeFailure|TestContextWindowClassifier|TestQuotaClassifier' -v`
Expected: FAIL with `undefined: llm.CodeRateLimit` etc.

**Step 3: Write minimal implementation**

Create `server/internal/harness/llm/failure.go`:

```go
package llm

import (
    "errors"
    "regexp"

    "hsg/internal/harness/services"
)

type FailureCode string

const (
    CodeAborted              FailureCode = "ABORTED"
    CodeAccountQuota         FailureCode = "ACCOUNT_QUOTA"
    CodeContextWindow        FailureCode = "CONTEXT_WINDOW_EXCEEDED"
    CodeEmptyResponse        FailureCode = "EMPTY_RESPONSE"
    CodeInvalidCredential    FailureCode = "INVALID_CREDENTIAL"
    CodeMalformedResponse    FailureCode = "MALFORMED_RESPONSE"
    CodeQuota                FailureCode = "QUOTA"
    CodeRateLimit            FailureCode = "RATE_LIMIT"
    CodeServer               FailureCode = "SERVER"
    CodeStreamClosed         FailureCode = "STREAM_CLOSED"
    CodeTimeout              FailureCode = "TIMEOUT"
    CodeTransport            FailureCode = "TRANSPORT"
    CodeUnknown              FailureCode = "UNKNOWN"
)

type LlmFailure struct {
    Message string
    Code    string
}

func NewFailure(message string, code FailureCode, cause error) *services.HarnessError {
    return services.NewHarnessError(message, string(code), cause)
}

func NormalizeFailure(err error) LlmFailure {
    if err == nil {
        return LlmFailure{Message: "LLM adapter failed", Code: string(CodeUnknown)}
    }
    var target *services.HarnessError
    if errors.As(err, &target) && target.Code != "" {
        return LlmFailure{Message: target.Message, Code: target.Code}
    }
    return LlmFailure{Message: err.Error(), Code: string(CodeUnknown)}
}

var (
    structuredContextOverflow = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])context[\s_-](?:length|window)[\s_-](?:exceed(?:ed|s)?|overflow(?:ed)?|limit[\s_-]exceeded)(?:$|[^a-z0-9])`)
    maxContextWindow          = regexp.MustCompile(`(?i)\b(?:maximum|max)(?:\s+(?:allowed|supported))?\s+context\s+(?:length|window)\b`)
    tooLargeForContext        = regexp.MustCompile(`(?i)\b(?:request|prompt|input|messages?)\s+(?:is\s+|are\s+)?too\s+(?:large|long)\s+for\s+(?:(?:this|the)\s+)?(?:model(?:'s)?\s+)?context(?:\s+window)?\b`)
    tooLongForModel           = regexp.MustCompile(`(?i)\b(?:input|prompt|request)\s+(?:is\s+)?too\s+(?:long|large)\s+for\s+(?:this|the)\s+model\b`)
    exceedsModelContext       = regexp.MustCompile(`(?i)\b(?:input|prompt|request|messages?)\b.{0,40}\b(?:exceed(?:s|ed)?|overflows?|is\s+larger\s+than)\b.{0,40}\b(?:the\s+)?(?:model(?:'s)?\s+)?context(?:\s+(?:length|window))?\b`)
)

func IsContextWindowExceeded(detail string) bool {
    return structuredContextOverflow.MatchString(detail) ||
        maxContextWindow.MatchString(detail) ||
        tooLargeForContext.MatchString(detail) ||
        tooLongForModel.MatchString(detail) ||
        exceedsModelContext.MatchString(detail)
}

var (
    insufficientQuota = regexp.MustCompile(`(?i)\binsufficient[\s_-]+(?:quota|balance|credits?)\b`)
    quotaExhausted    = regexp.MustCompile(`(?i)\b(?:quota|usage[\s_-]+limit)[\s_-]+(?:exceeded|exhausted|reached)\b`)
    exceededQuota     = regexp.MustCompile(`(?i)\bexceed(?:ed|s)?[\s_-]+(?:(?:your|the)[\s_-]+)?(?:current[\s_-]+)?quota\b`)
    balanceExhausted  = regexp.MustCompile(`(?i)\b(?:balance|credits?)[\s_-]+(?:exhausted|depleted)\b`)
    outOfCredits      = regexp.MustCompile(`(?i)\bout[\s_-]+of[\s_-]+(?:credits?|budget)\b`)
)

func IsQuotaExceeded(detail string) bool {
    return insufficientQuota.MatchString(detail) ||
        quotaExhausted.MatchString(detail) ||
        exceededQuota.MatchString(detail) ||
        balanceExhausted.MatchString(detail) ||
        outOfCredits.MatchString(detail)
}
```

Note: the code constants above are the subset used by harness-go consumers today. DSH defines ~50 codes; add only codes with a real consumer (per DSH AGENTS.md: "Require a current owner and need"). `CodeUnknown` is the fallback for third-party errors. Do not port `errorChain` (diagnostic rendering) until a UI/log consumer exists.

**Step 4: Run test to verify it passes**

Run: `cd /d/Github/agent-workspace/harness-go/server && gofmt -w internal/harness/llm/failure.go internal/harness/llm/failure_test.go && go test ./internal/harness/llm/ -v`
Expected: PASS, all existing `adapter_test.go` tests still PASS.

**Step 5: Commit**

```bash
cd /d/Github/agent-workspace/harness-go
git add server/internal/harness/llm/failure.go server/internal/harness/llm/failure_test.go
python scripts/pre_commit_check.py files
trail check
git commit -m "feat(harness): port DSH LLM failure codes"
```

---

### Task 2: Sequence range encoding for audit JSONL

**Files:**
- Create: `server/internal/harness/core/seqranges.go`
- Test: `server/internal/harness/core/seqranges_test.go`
- Reference: `deepseek-harness/packages/core/session/src/seq-ranges.ts`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type EncodedSeq struct { Start int; End int; Range bool }` — `Range=false` is a single value (`Start`), `Range=true` is the inclusive `[Start,End]` pair.
  - `func EncodeSeqRanges(values []int) []EncodedSeq`
  - `func DecodeSeqRanges(value any, maxEntries int) ([]int, error)`

**Step 1: Write the failing test**

```go
package core_test

import (
    "reflect"
    "testing"

    "hsg/internal/harness/core"
)

func TestEncodeSeqRangesCompressesRunsOfThree(t *testing.T) {
    got := core.EncodeSeqRanges([]int{1, 2, 3, 7, 8})
    want := []core.EncodedSeq{
        {Start: 1, End: 3, Range: true},
        {Start: 7, End: 7},
        {Start: 8, End: 8},
    }
    if !reflect.DeepEqual(got, want) {
        t.Fatalf("got=%+v want=%+v", got, want)
    }
}

func TestEncodeSeqRangesKeepsUnsortedAsIs(t *testing.T) {
    got := core.EncodeSeqRanges([]int{3, 1, 2})
    want := []core.EncodedSeq{{Start: 3, End: 3}, {Start: 1, End: 1}, {Start: 2, End: 2}}
    if !reflect.DeepEqual(got, want) {
        t.Fatalf("got=%+v want=%+v", got, want)
    }
}

func TestDecodeSeqRangesRoundTrip(t *testing.T) {
    encoded := core.EncodeSeqRanges([]int{1, 2, 3, 7, 8})
    raw := make([]any, len(encoded))
    for i, e := range encoded {
        if e.Range {
            raw[i] = []any{float64(e.Start), float64(e.End)}
        } else {
            raw[i] = float64(e.Start)
        }
    }
    got, err := core.DecodeSeqRanges(raw, 100)
    if err != nil {
        t.Fatal(err)
    }
    want := []int{1, 2, 3, 7, 8}
    if !reflect.DeepEqual(got, want) {
        t.Fatalf("got=%v want=%v", got, want)
    }
}

func TestDecodeSeqRangesRejectsInvalidInput(t *testing.T) {
    cases := []struct {
        name string
        in   any
        max  int
    }{
        {"not array", map[string]any{}, 10},
        {"negative", []any{float64(-1)}, 10},
        {"non integer", []any{1.5}, 10},
        {"bad range", []any{[]any{float64(2), float64(1)}}, 10},
        {"over max", []any{[]any{float64(1), float64(20)}}, 10},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            if _, err := core.DecodeSeqRanges(tc.in, tc.max); err == nil {
                t.Fatal("expected error")
            }
        })
    }
}
```

**Step 2: Run test to verify it fails**

Run: `cd /d/Github/agent-workspace/harness-go/server && go test ./internal/harness/core/ -run 'TestEncodeSeqRanges|TestDecodeSeqRanges' -v`
Expected: FAIL with `undefined: core.EncodedSeq`.

**Step 3: Write minimal implementation**

Create `server/internal/harness/core/seqranges.go`:

```go
package core

import (
    "fmt"
    "math"
)

type EncodedSeq struct {
    Start int
    End   int
    Range bool
}

func strictlyIncreasing(values []int) bool {
    for i := 1; i < len(values); i++ {
        if values[i] <= values[i-1] {
            return false
        }
    }
    return true
}

func EncodeSeqRanges(values []int) []EncodedSeq {
    if !strictlyIncreasing(values) {
        out := make([]EncodedSeq, len(values))
        for i, v := range values {
            out[i] = EncodedSeq{Start: v, End: v}
        }
        return out
    }
    var out []EncodedSeq
    for start := 0; start < len(values); {
        end := start
        for end+1 < len(values) && values[end+1] == values[end]+1 {
            end++
        }
        if end-start >= 2 {
            out = append(out, EncodedSeq{Start: values[start], End: values[end], Range: true})
        } else {
            for i := start; i <= end; i++ {
                out = append(out, EncodedSeq{Start: values[i], End: values[i]})
            }
        }
        start = end + 1
    }
    return out
}

func asSeq(v any) (int, bool) {
    f, ok := v.(float64)
    if !ok || f < 0 || f > math.MaxInt || math.Trunc(f) != f {
        return 0, false
    }
    return int(f), true
}

func DecodeSeqRanges(value any, maxEntries int) ([]int, error) {
    arr, ok := value.([]any)
    if !ok {
        return nil, fmt.Errorf("sourceEventSeqs must be an array")
    }
    var decoded []int
    hasRange := false
    for _, entry := range arr {
        if num, ok := asSeq(entry); ok {
            if len(decoded) >= maxEntries {
                return nil, fmt.Errorf("sourceEventSeqs exceeds its event sequence")
            }
            decoded = append(decoded, num)
            continue
        }
        pair, ok := entry.([]any)
        if !ok || len(pair) != 2 {
            return nil, fmt.Errorf("sourceEventSeqs range entries must be [start, end] pairs")
        }
        start, okStart := asSeq(pair[0])
        end, okEnd := asSeq(pair[1])
        if !okStart || !okEnd || end < start {
            return nil, fmt.Errorf("invalid sourceEventSeqs range")
        }
        if end-start+1 > maxEntries-len(decoded) {
            return nil, fmt.Errorf("sourceEventSeqs range exceeds its event sequence")
        }
        for seq := start; seq <= end; seq++ {
            decoded = append(decoded, seq)
        }
        hasRange = true
    }
    if hasRange && !strictlyIncreasing(decoded) {
        return nil, fmt.Errorf("sourceEventSeqs ranges must be strictly increasing")
    }
    return decoded, nil
}
```

Note: JSON numbers decode to `float64` via `encoding/json`. `asSeq` accepts only non-negative integral floats within `int` range, mirroring DSH's `Number.isSafeInteger` guard. On 64-bit Go, `int` is 64-bit; `math.MaxInt` bounds it.

**Step 4: Run test to verify it passes**

Run: `cd /d/Github/agent-workspace/harness-go/server && gofmt -w internal/harness/core/seqranges.go internal/harness/core/seqranges_test.go && go test ./internal/harness/core/ -v`
Expected: PASS.

**Step 5: Commit**

```bash
cd /d/Github/agent-workspace/harness-go
git add server/internal/harness/core/seqranges.go server/internal/harness/core/seqranges_test.go
python scripts/pre_commit_check.py files
trail check
git commit -m "feat(harness): port DSH seq range encoding"
```

---

### Task 3: Open-turn tool-call repair

**Files:**
- Create: `server/internal/harness/core/repair.go`
- Test: `server/internal/harness/core/repair_test.go`
- Reference: `deepseek-harness/packages/core/session/src/repair.ts`

**Interfaces:**
- Consumes: nothing new (pure types).
- Produces:
  - `const ToolNotStarted = "TOOL_NOT_STARTED"` and `ToolOutcomeUnknown = "TOOL_OUTCOME_UNKNOWN"`.
  - `type CloseCause string` with `CauseInterrupted CloseCause = "interrupted"`, `CauseForked CloseCause = "forked"`.
  - `type RepairEvent struct { Type string; Seq int; CallID string; Turn int; Step int; Started bool; Time int64 }` — minimal projection: `Type` ∈ `turn/start`, `turn/end`, `step/start`, `step/end`, `tool-call`, `tool-result`, `assistant-tool-call`. Other types are ignored.
  - `type Closer struct { Seq int; CallID string; Code string; Message string; Time int64; SourceEventSeq int }` — `SourceEventSeq=0` means no recorded call start.
  - `func OpenTurnClosers(events []RepairEvent, cause CloseCause) []Closer`
  - `func InterruptedTurnClosers(events []RepairEvent) []Closer` — delegates with `CauseInterrupted`.

  Simplify by folding the real event stream into `RepairEvent`s at the caller; this task ports the pure recovery fold, not the full Session event schema.

**Step 1: Write the failing test**

```go
package core_test

import (
    "testing"

    "hsg/internal/harness/core"
)

func TestOpenTurnClosersClosesStartedAndNotStartedCalls(t *testing.T) {
    events := []core.RepairEvent{
        {Type: "turn/start", Seq: 1, Turn: 1, Step: 1, Time: 100},
        {Type: "assistant-tool-call", Seq: 2, CallID: "a", Turn: 1, Step: 1, Time: 100},
        {Type: "tool-call", Seq: 3, CallID: "a", Turn: 1, Step: 1, Started: true, Time: 100},
        {Type: "assistant-tool-call", Seq: 4, CallID: "b", Turn: 1, Step: 1, Time: 100},
    }
    got := core.InterruptedTurnClosers(events)
    if len(got) != 2 {
        t.Fatalf("closers=%+v want 2", got)
    }
    if got[0].Code != core.ToolOutcomeUnknown || got[0].CallID != "a" || got[0].SourceEventSeq != 3 {
        t.Errorf("closer[0]=%+v", got[0])
    }
    if got[1].Code != core.ToolNotStarted || got[1].CallID != "b" || got[1].SourceEventSeq != 0 {
        t.Errorf("closer[1]=%+v", got[1])
    }
    for _, c := range got {
        if c.Message == "" {
            t.Errorf("closer for %s has no model-visible text", c.CallID)
        }
    }
}

func TestOpenTurnClosersBalancedLogReturnsNone(t *testing.T) {
    events := []core.RepairEvent{
        {Type: "turn/start", Seq: 1, Turn: 1, Step: 1, Time: 100},
        {Type: "assistant-tool-call", Seq: 2, CallID: "a", Turn: 1, Step: 1, Time: 100},
        {Type: "tool-result", Seq: 3, CallID: "a", Turn: 1, Step: 1, Time: 100},
        {Type: "turn/end", Seq: 4, Turn: 1, Step: 1, Time: 100},
    }
    if got := core.InterruptedTurnClosers(events); len(got) != 0 {
        t.Fatalf("closers=%+v want none", got)
    }
}

func TestOpenTurnClosersSequencesContinueLog(t *testing.T) {
    events := []core.RepairEvent{
        {Type: "turn/start", Seq: 7, Turn: 1, Step: 1, Time: 100},
        {Type: "assistant-tool-call", Seq: 8, CallID: "a", Turn: 1, Step: 1, Time: 100},
    }
    got := core.InterruptedTurnClosers(events)
    if len(got) != 1 || got[0].Seq != 9 {
        t.Fatalf("closers=%+v want seq 9", got)
    }
    if got[0].Time != 100 {
        t.Errorf("time=%d want last event time 100", got[0].Time)
    }
}
```

**Step 2: Run test to verify it fails**

Run: `cd /d/Github/agent-workspace/harness-go/server && go test ./internal/harness/core/ -run 'TestOpenTurnClosers' -v`
Expected: FAIL with `undefined: core.ToolOutcomeUnknown`.

**Step 3: Write minimal implementation**

Create `server/internal/harness/core/repair.go`:

```go
package core

const (
    ToolNotStarted     = "TOOL_NOT_STARTED"
    ToolOutcomeUnknown = "TOOL_OUTCOME_UNKNOWN"
)

type CloseCause string

const (
    CauseInterrupted CloseCause = "interrupted"
    CauseForked      CloseCause = "forked"
)

type RepairEvent struct {
    Type         string
    Seq          int
    CallID       string
    Turn         int
    Step         int
    Started      bool
    Time         int64
}

type Closer struct {
    Seq            int
    CallID         string
    Code           string
    Message        string
    Time           int64
    SourceEventSeq int
}

type pendingCall struct {
    turn     int
    step     int
    callSeq  int
    hasStart bool
}

func closerText(cause CloseCause, started bool) string {
    if cause == CauseForked {
        if started {
            return "The inherited history records this tool call starting but not its result. Retry only if read-only or idempotent; otherwise verify external state or ask the user before retrying."
        }
        return "The inherited history has no record of this tool call starting. Retry only if read-only or idempotent; otherwise verify external state or ask the user before retrying."
    }
    if started {
        return "The tool call was interrupted after it was recorded, but no result was durably recorded. Its outcome is unknown. Retry only if read-only or idempotent; otherwise verify external state or ask the user before retrying."
    }
    return "The tool call was interrupted before it was recorded as started. Retry it if it is still needed."
}

func OpenTurnClosers(events []RepairEvent, cause CloseCause) []Closer {
    pending := make(map[string]pendingCall)
    var order []string
    openTurn := false
    var lastSeq int
    var lastTime int64

    for _, ev := range events {
        lastSeq, lastTime = ev.Seq, ev.Time
        switch ev.Type {
        case "turn/start":
            openTurn = true
            pending = make(map[string]pendingCall)
            order = nil
        case "turn/end":
            openTurn = false
            pending = make(map[string]pendingCall)
            order = nil
        case "assistant-tool-call":
            if _, exists := pending[ev.CallID]; !exists {
                order = append(order, ev.CallID)
            }
            pending[ev.CallID] = pendingCall{turn: ev.Turn, step: ev.Step}
        case "tool-call":
            if p, exists := pending[ev.CallID]; exists {
                p.callSeq = ev.Seq
                p.hasStart = true
                pending[ev.CallID] = p
            }
        case "tool-result":
            delete(pending, ev.CallID)
        }
    }

    if !openTurn || len(order) == 0 {
        return nil
    }
    closers := make([]Closer, 0, len(order))
    seq := lastSeq + 1
    for _, callID := range order {
        p := pending[callID]
        code := ToolNotStarted
        sourceSeq := 0
        if p.hasStart {
            code = ToolOutcomeUnknown
            sourceSeq = p.callSeq
        }
        closers = append(closers, Closer{
            Seq:            seq,
            CallID:         callID,
            Code:           code,
            Message:        closerText(cause, p.hasStart),
            Time:           lastTime,
            SourceEventSeq: sourceSeq,
        })
        seq++
    }
    return closers
}

func InterruptedTurnClosers(events []RepairEvent) []Closer {
    return OpenTurnClosers(events, CauseInterrupted)
}
```

Note: DSH also emits synthetic `step/end` and `turn/end` events and distinguishes `surfaceOp`. This port emits only the tool-result closers — the part that repairs the model-visible protocol violation (dangling assistant calls). Turn/step lifecycle closers are a separate concern; add them only when a consumer needs the replayed transcript to be structurally closed.

**Step 4: Run test to verify it passes**

Run: `cd /d/Github/agent-workspace/harness-go/server && gofmt -w internal/harness/core/repair.go internal/harness/core/repair_test.go && go test ./internal/harness/core/ -v`
Expected: PASS.

**Step 5: Commit**

```bash
cd /d/Github/agent-workspace/harness-go
git add server/internal/harness/core/repair.go server/internal/harness/core/repair_test.go
python scripts/pre_commit_check.py files
trail check
git commit -m "feat(harness): port DSH open-turn tool-call repair"
```

---

### Task 4: Compaction provenance (checkpoint + tool pairing)

**Files:**
- Create: `server/internal/harness/core/compaction.go`
- Test: `server/internal/harness/core/compaction_test.go`
- Reference: `deepseek-harness/packages/compaction/compaction/src/checkpoint.ts`, `tool-pairing.ts`

**Interfaces:**
- Consumes: `core.RepairEvent` (Task 3).
- Produces:
  - `type MessageSource struct { Kind string; CompactionID string; SourceCommandID string }`
  - `func CompactCheckpointSource(compactionID, sourceCommandID string) MessageSource` — `Kind="compact-checkpoint"`, omit `SourceCommandID` when empty.
  - `func IsCompactCheckpointSource(source MessageSource) bool`
  - `func ToolPairingBalanced(events []RepairEvent, beforeIndex int) bool` — `beforeIndex` ∈ `[0, len(events)]`; returns whether no tool-call crosses that cut. Equivalent to DSH `toolPairingBalancedAfter(events[beforeIndex-1])` when `beforeIndex>0`, and the leading cut when `beforeIndex==0`.

**Step 1: Write the failing test**

```go
package core_test

import (
    "testing"

    "hsg/internal/harness/core"
)

func TestCompactCheckpointSource(t *testing.T) {
    src := core.CompactCheckpointSource("cmp-1", "cmd-2")
    if !core.IsCompactCheckpointSource(src) {
        t.Fatal("not recognized as checkpoint")
    }
    if src.Kind != "compact-checkpoint" || src.CompactionID != "cmp-1" || src.SourceCommandID != "cmd-2" {
        t.Fatalf("source=%+v", src)
    }
    empty := core.CompactCheckpointSource("cmp-1", "")
    if empty.SourceCommandID != "" {
        t.Errorf("empty command id should stay empty: %+v", empty)
    }
    if core.IsCompactCheckpointSource(core.MessageSource{Kind: "tool"}) {
        t.Error("tool source misclassified")
    }
}

func TestToolPairingBalanced(t *testing.T) {
    events := []core.RepairEvent{
        {Type: "assistant-tool-call", CallID: "a", Seq: 1},
        {Type: "tool-result", CallID: "a", Seq: 2},
    }
    if !core.ToolPairingBalanced(events, 0) {
        t.Error("leading cut should be balanced")
    }
    if core.ToolPairingBalanced(events, 1) {
        t.Error("cut between call and result must be unbalanced")
    }
    if !core.ToolPairingBalanced(events, 2) {
        t.Error("trailing cut should be balanced")
    }
}

func TestToolPairingBalancedRejectsResultWithoutCall(t *testing.T) {
    events := []core.RepairEvent{{Type: "tool-result", CallID: "a", Seq: 1}}
    defer func() {
        if recover() == nil {
            t.Fatal("expected panic on corrupt pairing")
        }
    }()
    core.ToolPairingBalanced(events, 1)
}
```

**Step 2: Run test to verify it fails**

Run: `cd /d/Github/agent-workspace/harness-go/server && go test ./internal/harness/core/ -run 'TestCompactCheckpointSource|TestToolPairingBalanced' -v`
Expected: FAIL with `undefined: core.CompactCheckpointSource`.

**Step 3: Write minimal implementation**

Create `server/internal/harness/core/compaction.go`:

```go
package core

import "fmt"

const checkpointKind = "compact-checkpoint"

type MessageSource struct {
    Kind            string
    CompactionID    string
    SourceCommandID string
}

func CompactCheckpointSource(compactionID, sourceCommandID string) MessageSource {
    return MessageSource{
        Kind:            checkpointKind,
        CompactionID:    compactionID,
        SourceCommandID: sourceCommandID,
    }
}

func IsCompactCheckpointSource(source MessageSource) bool {
    return source.Kind == checkpointKind
}

func ToolPairingBalanced(events []RepairEvent, beforeIndex int) bool {
    if beforeIndex < 0 || beforeIndex > len(events) {
        panic(fmt.Sprintf("tool-pairing balance: index %d out of range", beforeIndex))
    }
    open := 0
    for i := 0; i < beforeIndex; i++ {
        switch events[i].Type {
        case "assistant-tool-call":
            open++
        case "tool-result":
            open--
            if open < 0 {
                panic(fmt.Sprintf("tool-pairing balance: tool/result at seq %d has no matching tool-call", events[i].Seq))
            }
        }
    }
    return open == 0
}
```

Note: DSH caches balance per surface generation with a `WeakMap`. This port computes the fold directly over `[]RepairEvent` — O(n) per query. Add a cache only if profiling shows the O(n) cost matters; a premature cache would need generation invalidation the harness does not yet have.

**Step 4: Run test to verify it passes**

Run: `cd /d/Github/agent-workspace/harness-go/server && gofmt -w internal/harness/core/compaction.go internal/harness/core/compaction_test.go && go test ./internal/harness/core/ -v`
Expected: PASS.

**Step 5: Commit**

```bash
cd /d/Github/agent-workspace/harness-go
git add server/internal/harness/core/compaction.go server/internal/harness/core/compaction_test.go
python scripts/pre_commit_check.py files
trail check
git commit -m "feat(harness): port DSH compaction provenance"
```

---

## Final Verification (after all 4 tasks)

```bash
cd /d/Github/agent-workspace/harness-go/server
go test ./...
cd ..
python scripts/test_pre_commit_check.py
```

Expected: all Go tests PASS, checker tests exit 0. Then push:

```bash
git push origin main
```

## Deferred (not 1:1 / no current consumer)

- `errorChain` diagnostic rendering — no UI/log consumer yet.
- Full DSH code table (~50 codes) — add only codes with a real consumer.
- Session schema event types (`tool/call`, `tool/result`, turn/step lifecycle) — harness-go has no durable session log; repair operates on a minimal `RepairEvent` projection.
- Tool-pairing balance cache — O(n) fold is sufficient until a profiling signal exists.
