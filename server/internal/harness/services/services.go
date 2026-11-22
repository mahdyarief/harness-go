package services

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"hsg/internal/harness/evidence"
	"hsg/internal/harness/scope"
	"hsg/internal/lib/agent"
	"hsg/internal/lib/ai"
)

var (
	ErrLoopMissing = errors.New("agentLoop service missing")
	ErrDenied      = errors.New("tool denied")
	// ErrToolBudget terminates the loop once every remaining tool call has
	// been answered with a synthetic not-started result.
	ErrToolBudget = NewHarnessError("tool budget exhausted", "TOOL_BUDGET_EXHAUSTED", nil)
	// ErrTokenBudget terminates the loop before the next model request when
	// the accumulated completion tokens exceed MaxTotalTokens.
	ErrTokenBudget = NewHarnessError("token budget exhausted", "TOKEN_BUDGET_EXHAUSTED", nil)
	// ErrTurnBudget terminates the loop when MaxTurns model calls ran without
	// a final answer (legacy runner: "turn budget exhausted").
	ErrTurnBudget = NewHarnessError("turn budget exhausted", "TURN_BUDGET_EXHAUSTED", nil)
)

const CodeToolNotStarted = "TOOL_NOT_STARTED"

// HarnessError carries a stable machine-routable code distinct from the
// human-readable message, with a chained cause. Route on Code, never on text.
type HarnessError struct {
	Code    string
	Message string
	Cause   error
}

func NewHarnessError(message, code string, cause error) *HarnessError {
	return &HarnessError{Code: code, Message: message, Cause: cause}
}

func (e *HarnessError) Error() string { return e.Message }
func (e *HarnessError) Unwrap() error { return e.Cause }

// ErrorCode returns the stable code for a HarnessError, or "" for other errors.
func ErrorCode(err error) string {
	var target *HarnessError
	if errors.As(err, &target) {
		return target.Code
	}
	return ""
}

type RunRequest struct {
	Messages   []ai.Message
	ToolNames  []string
	SystemText string
}

type Sink func(AIEvent) error

// GuardAction is what a guard wants the harness to do after a turn.
type GuardAction int

const (
	GuardContinue GuardAction = iota
	GuardStop
)

// GuardVerdict is a guard's decision. Reason is user-facing Indonesian text and
// must not contain tool output, arguments, or provider errors.
type GuardVerdict struct {
	Action GuardAction
	Reason string
}

func Allow() GuardVerdict             { return GuardVerdict{} }
func Stop(reason string) GuardVerdict { return GuardVerdict{Action: GuardStop, Reason: reason} }
func (v GuardVerdict) Allowed() bool  { return v.Action == GuardContinue }

// GuardMemory is per-run scratch space, unique per guard.
type GuardMemory map[string]int

func (m GuardMemory) Incr(key string) int { m[key]++; return m[key] }

// GuardTurn is what a LoopGuard sees after a turn's tools have run.
type GuardTurn struct {
	Turn    int
	Records []ToolRecord
	History []ToolRecord
}

// LoopGuard inspects one finished turn and may end the run gracefully.
type LoopGuard interface {
	Name() string
	Inspect(mem GuardMemory, turn GuardTurn) GuardVerdict
}

type AIEvent struct {
	Type       string `json:"type"`
	Agent      string `json:"agent,omitempty"`
	Text       string `json:"text,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	ToolCall   string `json:"tool_call,omitempty"`
	Error      string `json:"error,omitempty"`
	DurationMs int    `json:"duration_ms,omitempty"`
	// Payload carries the typed body of audit events (model/call,
	// tool/result, structured/submit) consumed by the run log adapter.
	Payload json.RawMessage `json:"payload,omitempty"`
}

// ToolRecord is the audit row for one invocation. It is produced by the loop so
// the handler never has to reconstruct which tool ran and what it returned.
type ToolRecord struct {
	Turn       int
	Name       string
	Arguments  string
	Result     string
	DurationMs int
	Err        string
	References []Reference
}

type Outcome struct {
	Content          string
	PromptTokens     int
	CompletionTokens int
	Turns            int
	ToolCalls        int
	Tools            []ToolRecord
	References       []Reference
	DurationMs       int
	Partial          bool
	// Structured is the validated argument JSON of the terminal tool call
	// that ended the run, when one was made.
	Structured json.RawMessage
	// Evidence is every typed provenance record produced by the run's tools,
	// ordered by first appearance. Structured citations are grounded against
	// it before the run ends.
	Evidence []evidence.Item
}

// LoopConfig bounds one loop run. Zero values are normalized by the loop so a
// caller cannot accidentally run without limits.
type LoopConfig struct {
	MaxTurns                int
	MaxToolCalls            int
	MaxDuration             time.Duration
	MaxTotalTokens          int
	ToolResultBytes         int
	MaxToolResultBytes      int
	MaxToolConcurrency      int
	ToolTimeout             time.Duration
	ContextTokenCap         int
	KeepLastMessages        int
	MaxRetries              int
	MaxContinuationNudges   int
	MaxPartialContinuations int
	ForceToolCall           string
	ReminderThresholds      []int
	ReminderPreviewChars    int
	ReminderInclude         []string
	ReminderExclude         []string
	LoopGuards              []LoopGuard
	// RequireStructured names the tool the model must submit. Empty keeps the
	// legacy behaviour of ending on a text answer.
	RequireStructured    string
	MaxStructuredRepairs int
	// EvidenceRegistry lists the evidence kinds tools may produce. When set,
	// a structured submission citing a ref no tool returned is rejected with
	// EVIDENCE_UNGROUNDED and fed back as a repair.
	EvidenceRegistry *evidence.Registry
	// EvidencePath is the dotted path inside the structured submission that
	// holds the citations, e.g. "evidence" or "result.evidence". Empty
	// searches the whole submission.
	EvidencePath string
}

type ResultSpec struct {
	Name   string
	Schema json.RawMessage
}

type Agent interface {
	Run(ctx context.Context, req RunRequest, sink Sink) (Outcome, error)
}

type AgentLoop interface {
	Run(ctx context.Context, req RunRequest, sink Sink) (Outcome, error)
}

type Reference struct {
	DocumentID  string `json:"document_id"`
	ChunkID     string `json:"chunk_id"`
	Title       string `json:"title"`
	Filename    string `json:"filename"`
	Format      string `json:"format"`
	Locator     string `json:"locator"`
	HeadingPath string `json:"heading_path"`
	Position    int    `json:"position"`
	Excerpt     string `json:"excerpt"`
	Source      string `json:"source,omitempty"`
}

type ToolResult struct {
	Output string
	// References is the KB passage provenance of the output. It is mapped to
	// DOCUMENT evidence items; new tools should prefer Evidence directly.
	References []Reference
	// Evidence carries typed provenance for every claim the output grounds:
	// documents, rules, graph edges, service records. The loop collects it
	// into Outcome.Evidence and grounds structured citations against it.
	Evidence []evidence.Item
}

// DocumentItem maps a KB Reference to a DOCUMENT evidence item: the chunk id
// (or document id when no chunk) is the Ref, all other fields survive in Meta
// so no data is lost.
func DocumentItem(ref Reference) evidence.Item {
	meta := map[string]string{
		"document_id":  ref.DocumentID,
		"title":        ref.Title,
		"filename":     ref.Filename,
		"format":       ref.Format,
		"locator":      ref.Locator,
		"heading_path": ref.HeadingPath,
		"position":     strconv.Itoa(ref.Position),
	}
	if ref.Source != "" {
		meta["source"] = ref.Source
	}
	chunk := ref.ChunkID
	if chunk == "" {
		chunk = ref.DocumentID
	}
	return evidence.Item{Kind: "DOCUMENT", Ref: chunk, Excerpt: ref.Excerpt, Meta: meta}
}

type Tool interface {
	Name() string
	Description() string
	Parameters() json.RawMessage
	ReadOnly() bool
	Execute(ctx context.Context, args json.RawMessage) (ToolResult, error)
}

type Tools interface {
	Lookup(name string) (Tool, bool)
	Specs() []ai.ToolSpec
}

// Terminal is an optional marker for a tool whose successful call ends the
// run without another model turn. Its validated arguments become
// Outcome.Structured.
type Terminal interface {
	Terminal() bool
}

type LLM interface {
	MaxTokens() int
	ChatStream(ctx context.Context, messages []ai.Message, opts ai.ChatOptions, onDelta func(ai.StreamDelta) error) (ai.ChatResult, error)
}

// RunRecord is the row-level metadata of one harness run. ParentRunID is set
// when a team run dispatches a member, so every run names its producer.
type RunRecord struct {
	ID               string
	TenantID         string
	ParentRunID      string
	ProfileName      string
	ProfileHash      string
	Model            string
	ConfigHash       string
	Status           string
	PromptTokens     int
	CompletionTokens int
	CostUSD          float64
	StartedAt        time.Time
	EndedAt          time.Time
}

// RunLogEvent is one append-only record in a run's event log. Seq is assigned
// by the store and is contiguous per run.
type RunLogEvent struct {
	RunID   string          `json:"run_id"`
	Seq     int64           `json:"seq"`
	Member  string          `json:"member,omitempty"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
	At      time.Time       `json:"at"`
}

// RunCompletion is the terminal state written to a run's row.
type RunCompletion struct {
	Status           string
	PromptTokens     int
	CompletionTokens int
	CostUSD          float64
	EndedAt          time.Time
}

// RunLog persists run metadata and its append-only event stream. Events()
// returns a run's events ordered by Seq.
type RunLog interface {
	CreateRun(ctx context.Context, run RunRecord) error
	AppendEvent(ctx context.Context, event RunLogEvent) error
	FinishRun(ctx context.Context, runID string, completion RunCompletion) error
	Events(ctx context.Context, runID string) ([]RunLogEvent, error)
}

type GuardDecision struct {
	Allowed bool
	Reason  string
}

// PreDecision is the outcome of the ordered pre-execute pipeline.
// Kind is one of "allow", "deny", "ask"; Code is a stable audit code.
type PreDecision struct {
	Kind   string
	Reason string
	Code   string
}

func PreAllow() PreDecision { return PreDecision{Kind: "allow"} }
func PreDeny(reason, code string) PreDecision {
	return PreDecision{Kind: "deny", Reason: reason, Code: code}
}

// PreExecute is one link in the tool pre-execute chain. Call next() to run the
// remaining links; returning without calling it short-circuits the chain.
type PreExecute func(ctx context.Context, call ToolInvocation, next func() PreDecision) PreDecision

// ToolInvocation is the normalized view of a tool call handed to pre-execute
// links. It carries no model-facing content beyond arguments.
type ToolInvocation struct {
	Name      string
	Arguments json.RawMessage
	ReadOnly  bool
}

// ApprovalAnswer resolves an "ask" decision interactively. Providing none
// makes every ask fail closed with APPROVAL_UNAVAILABLE, as in DSH.
type ApprovalAnswer func(ctx context.Context, call ToolInvocation) (bool, error)

// Preparer splits a tool call into DSH's prepare and dispatch phases. Prepare
// runs every pre-execute check and returns either a denial (a *HarnessError
// with a stable code) or a run function. The loop calls Prepare before it
// announces tool_start, so a denied call is never shown as started.
type Preparer interface {
	Prepare(ctx context.Context, args json.RawMessage) (func(context.Context) (ToolResult, error), error)
}

var (
	AgentKey         = scope.NewKey[Agent]("agent")
	AgentLoopKey     = scope.NewKey[AgentLoop]("agentLoop")
	LoopConfigKey    = scope.NewKey[LoopConfig]("loopConfig")
	ToolsKey         = scope.NewKey[Tools]("tools")
	AgentRegistryKey = scope.NewKey[*agent.Registry]("agentRegistry")
	LLMKey           = scope.NewKey[LLM]("llm")
	RunLogKey        = scope.NewKey[RunLog]("runLog")
	SystemPromptKey  = scope.NewKey[string]("systemPrompt")
	SandboxKey       = scope.NewKey[string]("sandboxPolicy")
	ApprovalKey      = scope.NewKey[string]("approvalPolicy")
	ToolAllowlistKey = scope.NewKey[[]string]("toolAllowlist")
	// ApprovalAnswerKey is optional; see ApprovalAnswer.
	ApprovalAnswerKey = scope.NewKey[ApprovalAnswer]("approvalAnswer")
)
