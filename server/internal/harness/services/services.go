package services

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"hsg/internal/harness/scope"
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
	Text       string `json:"text,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	ToolCall   string `json:"tool_call,omitempty"`
	Error      string `json:"error,omitempty"`
	DurationMs int    `json:"duration_ms,omitempty"`
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
	Output     string
	References []Reference
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

type LLM interface {
	MaxTokens() int
	ChatStream(ctx context.Context, messages []ai.Message, opts ai.ChatOptions, onDelta func(ai.StreamDelta) error) (ai.ChatResult, error)
}

type Sessions interface {
	Append(turn Outcome) error
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
	LLMKey           = scope.NewKey[LLM]("llm")
	SessionsKey      = scope.NewKey[Sessions]("sessions")
	SystemPromptKey  = scope.NewKey[string]("systemPrompt")
	GuardsKey        = scope.NewKey[[]GuardDecision]("guards")
	SandboxKey       = scope.NewKey[string]("sandboxPolicy")
	ApprovalKey      = scope.NewKey[string]("approvalPolicy")
	ToolAllowlistKey = scope.NewKey[[]string]("toolAllowlist")
	// ApprovalAnswerKey is optional; see ApprovalAnswer.
	ApprovalAnswerKey = scope.NewKey[ApprovalAnswer]("approvalAnswer")
)
