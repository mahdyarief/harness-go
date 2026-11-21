package agent

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"hsg/internal/lib/ai"
)

type fakeGateway struct {
	results []ai.ChatResult
	errs    []error
	// emitOnErr makes a failing attempt stream its result content first, which
	// models a provider cutting the stream mid-answer.
	emitOnErr bool
	calls     int32
	last      []ai.Message
	limit     int
	opts      []ai.ChatOptions
}

func (f *fakeGateway) MaxTokens() int {
	if f.limit > 0 {
		return f.limit
	}
	return 512
}

func (f *fakeGateway) ChatStream(_ context.Context, messages []ai.Message, opts ai.ChatOptions, onDelta func(ai.StreamDelta) error) (ai.ChatResult, error) {
	atomic.AddInt32(&f.calls, 1)
	f.last = append([]ai.Message(nil), messages...)
	f.opts = append(f.opts, opts)
	idx := int(atomic.LoadInt32(&f.calls)) - 1
	if idx < len(f.errs) && f.errs[idx] != nil {
		var result ai.ChatResult
		if idx < len(f.results) {
			result = f.results[idx]
		}
		if f.emitOnErr && result.Content != "" {
			if err := onDelta(ai.StreamDelta{Content: result.Content}); err != nil {
				return result, err
			}
		}
		return result, f.errs[idx]
	}
	if idx >= len(f.results) {
		return ai.ChatResult{}, errors.New("unexpected call")
	}
	result := f.results[idx]
	if result.Content != "" {
		if err := onDelta(ai.StreamDelta{Content: result.Content}); err != nil {
			return result, err
		}
	}
	return result, nil
}

type echoTool struct {
	name   string
	read   bool
	calls  int32
	fail   bool
	output string
}

func (t *echoTool) Name() string        { return t.name }
func (t *echoTool) Description() string { return "echoes its input" }
func (t *echoTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}},"required":["q"]}`)
}
func (t *echoTool) ReadOnly() bool { return t.read }
func (t *echoTool) Execute(_ context.Context, args json.RawMessage) (ToolResult, error) {
	atomic.AddInt32(&t.calls, 1)
	if t.fail {
		return ToolResult{}, errors.New("boom")
	}
	return ToolResult{Output: t.output + string(args)}, nil
}

func toolCallTurn(id, name, args string) ai.ChatResult {
	return ai.ChatResult{
		FinishReason:     "tool_calls",
		CompletionTokens: 10,
		ToolCalls: []ai.ToolCall{{
			ID:       id,
			Type:     "function",
			Function: ai.ToolCallFunction{Name: name, Arguments: args},
		}},
	}
}

func TestRunnerToolRoundTrip(t *testing.T) {
	tool := &echoTool{name: "echo", read: true, output: "ok:"}
	gateway := &fakeGateway{results: []ai.ChatResult{
		toolCallTurn("call_1", "echo", `{"q":"hi"}`),
		{FinishReason: "stop", Content: "done", CompletionTokens: 5},
	}}
	runner := newRunner(gateway, NewRegistry(tool), Config{})

	var events []string
	out, err := runner.Run(context.Background(), []ai.Message{{Role: "user", Content: "hello"}}, func(e Event) error {
		events = append(events, e.Type)
		return nil
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Content != "done" || out.Turns != 2 || out.ToolCalls != 1 {
		t.Fatalf("unexpected outcome: %+v", out)
	}
	if len(out.Tools) != 1 || out.Tools[0].Name != "echo" || out.Tools[0].Err != "" {
		t.Fatalf("unexpected tool records: %+v", out.Tools)
	}
	if atomic.LoadInt32(&tool.calls) != 1 {
		t.Fatalf("tool ran %d times, want 1", tool.calls)
	}
	// The tool-calling turn carries no text, so only the final turn emits delta.
	want := "tool_start,tool_end,delta"
	if strings.Join(events, ",") != want {
		t.Fatalf("events = %v, want %s", events, want)
	}
	// The second request must carry the assistant tool call and its result.
	last := gateway.last[len(gateway.last)-1]
	if last.Role != "tool" || last.ToolCallID != "call_1" {
		t.Fatalf("tool result not forwarded: %+v", last)
	}
}

func TestRunnerAccumulatesUsageOnFailure(t *testing.T) {
	stop := errors.New("sink failed")
	for _, tc := range []struct {
		name        string
		result      ai.ChatResult
		err         error
		sink        bool
		want        error
		wantContent string
		wantPartial bool
	}{
		{"provider", ai.ChatResult{PromptTokens: 7, CompletionTokens: 9}, ai.ErrProvider, false, ai.ErrProvider, "", false},
		{"parser_cap", ai.ChatResult{PromptTokens: 7, CompletionTokens: 600}, ai.ErrOutput, false, ai.ErrOutput, "", false},
		{"runner_cap", ai.ChatResult{Content: "secret", PromptTokens: 7, CompletionTokens: 600}, nil, false, ai.ErrOutput, "", false},
		// "sink": the fake gateway calls onDelta before returning the error, so
		// the streamed text "x" is captured and returned as a partial answer.
		{"sink", ai.ChatResult{Content: "x", PromptTokens: 7, CompletionTokens: 9}, nil, true, stop, "x", true},
		{"unknown", ai.ChatResult{}, ai.ErrProvider, false, ai.ErrProvider, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first := toolCallTurn("call_1", "unknown", `{}`)
			gateway := &fakeGateway{results: []ai.ChatResult{first, tc.result}, errs: []error{nil, tc.err}}
			runner := newRunner(gateway, NewRegistry(), Config{})
			out, err := runner.Run(context.Background(), []ai.Message{{Role: "user", Content: "x"}}, func(e Event) error {
				if tc.sink && e.Type == "delta" {
					return stop
				}
				return nil
			})
			if !errors.Is(err, tc.want) ||
				out.PromptTokens != first.PromptTokens+tc.result.PromptTokens ||
				out.CompletionTokens != first.CompletionTokens+tc.result.CompletionTokens ||
				out.Content != tc.wantContent ||
				out.Partial != tc.wantPartial ||
				gateway.calls != 2 {
				t.Fatalf("out=%+v err=%v calls=%d", out, err, gateway.calls)
			}
		})
	}
}

func TestRunnerCountsRetryUsageExactlyOnce(t *testing.T) {
	for _, succeeds := range []bool{false, true} {
		retry := &ai.ProviderError{Status: 503, Retryable: true}
		errs := []error{retry, retry}
		if succeeds {
			errs[1] = nil
		}
		gateway := &fakeGateway{results: []ai.ChatResult{
			{PromptTokens: 7, CompletionTokens: 3},
			{Content: "ok", FinishReason: "stop", PromptTokens: 11, CompletionTokens: 5},
		}, errs: errs}
		runner := newRunner(gateway, NewRegistry(), Config{MaxRetries: 1})
		out, err := runner.Run(context.Background(), []ai.Message{{Role: "user", Content: "x"}}, nil)
		if (err == nil) != succeeds || out.PromptTokens != 18 || out.CompletionTokens != 8 || gateway.calls != 2 {
			t.Fatalf("succeeds=%v: out=%+v err=%v calls=%d", succeeds, out, err, gateway.calls)
		}
	}
}

func TestRunnerResumesCutStreamToFullAnswer(t *testing.T) {
	// First attempt is cut mid-text (ErrOutput from a stream without [DONE]),
	// carrying partial content. The runner must resume and return the complete
	// answer, not a partial one, with the carried text leading the final
	// content and no hard error.
	gateway := &fakeGateway{results: []ai.ChatResult{
		{Content: "The first half of the answer.", CompletionTokens: 12, FinishReason: "stop"},
		{Content: " The second half of the answer.", PromptTokens: 9, CompletionTokens: 8, FinishReason: "stop"},
	}, errs: []error{ai.ErrOutput, nil}, emitOnErr: true}
	runner := newRunner(gateway, NewRegistry(), Config{})

	var events []string
	out, err := runner.Run(context.Background(), []ai.Message{{Role: "user", Content: "x"}}, func(e Event) error {
		events = append(events, e.Type)
		return nil
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Partial {
		t.Fatalf("out.Partial = %v, want false", out.Partial)
	}
	if out.Content != "The first half of the answer. The second half of the answer." {
		t.Fatalf("out.Content = %q", out.Content)
	}
	if gateway.calls != 2 {
		t.Fatalf("gateway.calls = %d, want 2", gateway.calls)
	}
	// The resume re-prompt must be forwarded to the second request.
	second := gateway.last[len(gateway.last)-1]
	if second.Role != "user" || !strings.Contains(second.Content, "Lanjutkan jawaban") {
		t.Fatalf("resume prompt not forwarded: %+v", second)
	}
	if !slices.Contains(events, "notice") {
		t.Fatalf("events = %v, want notice", events)
	}
}

func TestRunnerUnknownToolBecomesText(t *testing.T) {
	gateway := &fakeGateway{results: []ai.ChatResult{
		toolCallTurn("call_1", "delete_everything", ``),
		{FinishReason: "stop", Content: "cannot", CompletionTokens: 5},
	}}
	runner := newRunner(gateway, NewRegistry(), Config{})

	out, err := runner.Run(context.Background(), []ai.Message{{Role: "user", Content: "x"}}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Content != "cannot" {
		t.Fatalf("content = %q", out.Content)
	}
	if len(out.Tools) != 1 || out.Tools[0].Err == "" {
		t.Fatalf("expected an audit record with an error: %+v", out.Tools)
	}
	if !strings.Contains(gateway.last[len(gateway.last)-1].Content, "not available") {
		t.Fatalf("model did not receive a rejection: %q", gateway.last[len(gateway.last)-1].Content)
	}
}

func TestRunnerRejectsInvalidArguments(t *testing.T) {
	tool := &echoTool{name: "echo", read: true}
	gateway := &fakeGateway{results: []ai.ChatResult{
		toolCallTurn("call_1", "echo", `{"other":"x"}`),
		{FinishReason: "stop", Content: "bad args", CompletionTokens: 5},
	}}
	runner := newRunner(gateway, NewRegistry(tool), Config{})

	if _, err := runner.Run(context.Background(), []ai.Message{{Role: "user", Content: "x"}}, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if atomic.LoadInt32(&tool.calls) != 0 {
		t.Fatalf("tool ran despite invalid arguments")
	}
}

func TestRunnerToolFailureIsRecoverable(t *testing.T) {
	tool := &echoTool{name: "echo", read: true, fail: true}
	gateway := &fakeGateway{results: []ai.ChatResult{
		toolCallTurn("call_1", "echo", `{"q":"hi"}`),
		{FinishReason: "stop", Content: "recovered", CompletionTokens: 5},
	}}
	runner := newRunner(gateway, NewRegistry(tool), Config{})

	out, err := runner.Run(context.Background(), []ai.Message{{Role: "user", Content: "x"}}, nil)
	if err != nil {
		t.Fatalf("a tool error must not abort the run: %v", err)
	}
	if out.Content != "recovered" || len(out.Tools) != 1 || out.Tools[0].Err == "" {
		t.Fatalf("unexpected outcome: %+v", out)
	}
}

func TestRunnerProviderFailureReturnsError(t *testing.T) {
	gateway := &fakeGateway{errs: []error{&ai.ProviderError{Status: 500, Retryable: false}}}
	runner := newRunner(gateway, NewRegistry(), Config{})

	_, err := runner.Run(context.Background(), []ai.Message{{Role: "user", Content: "x"}}, nil)
	if !errors.Is(err, ai.ErrProvider) {
		t.Fatalf("err = %v, want ErrProvider", err)
	}
	if atomic.LoadInt32(&gateway.calls) != 1 {
		t.Fatalf("non-retryable error was retried: %d calls", gateway.calls)
	}
}

func TestRunnerRetriesRetryableProviderError(t *testing.T) {
	gateway := &fakeGateway{
		errs:    []error{&ai.ProviderError{Status: 503, Retryable: true}, nil},
		results: []ai.ChatResult{{}, {FinishReason: "stop", Content: "second try", CompletionTokens: 5}},
	}
	runner := newRunner(gateway, NewRegistry(), Config{MaxRetries: 1})

	out, err := runner.Run(context.Background(), []ai.Message{{Role: "user", Content: "x"}}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Content != "second try" || atomic.LoadInt32(&gateway.calls) != 2 {
		t.Fatalf("outcome=%+v calls=%d", out, gateway.calls)
	}
}

func TestRunnerTurnBudget(t *testing.T) {
	tool := &echoTool{name: "echo", read: true}
	gateway := &fakeGateway{results: []ai.ChatResult{
		toolCallTurn("call_1", "echo", `{"q":"a"}`),
		toolCallTurn("call_2", "echo", `{"q":"b"}`),
	}}
	runner := newRunner(gateway, NewRegistry(tool), Config{MaxTurns: 2, MaxTotalTokens: 8192})

	_, err := runner.Run(context.Background(), []ai.Message{{Role: "user", Content: "x"}}, nil)
	if !errors.Is(err, ai.ErrOutput) {
		t.Fatalf("err = %v, want ErrOutput", err)
	}
	if atomic.LoadInt32(&gateway.calls) != 2 {
		t.Fatalf("turn budget not enforced: %d calls", gateway.calls)
	}
}

func TestRunnerTokenBudget(t *testing.T) {
	gateway := &fakeGateway{results: []ai.ChatResult{
		toolCallTurn("call_1", "echo", `{"q":"a"}`),
		{FinishReason: "stop", Content: "too late", CompletionTokens: 9000},
	}}
	tool := &echoTool{name: "echo", read: true}
	runner := newRunner(gateway, NewRegistry(tool), Config{MaxTotalTokens: 100, MaxTurns: 4})

	_, err := runner.Run(context.Background(), []ai.Message{{Role: "user", Content: "x"}}, nil)
	if !errors.Is(err, ai.ErrOutput) {
		t.Fatalf("err = %v, want ErrOutput", err)
	}
}

func TestRunnerRemainingBudget(t *testing.T) {
	for _, budget := range []int{1, 16384, 32768} {
		first := min(budget, 16384)
		tool := toolCallTurn("call_1", "echo", `{"q":"a"}`)
		tool.CompletionTokens = first
		g := &fakeGateway{limit: 16384, results: []ai.ChatResult{tool, {FinishReason: "stop", Content: "done", CompletionTokens: budget - first}}}
		r := newRunner(g, NewRegistry(&echoTool{name: "echo", read: true}), Config{MaxTotalTokens: budget})
		out, err := r.Run(context.Background(), []ai.Message{{Role: "user", Content: "x"}}, nil)
		if budget == first {
			if !errors.Is(err, ai.ErrOutput) || len(g.opts) != 1 {
				t.Fatalf("budget %d: calls %d, err %v", budget, len(g.opts), err)
			}
		} else {
			if err != nil || out.CompletionTokens != budget || len(g.opts) != 2 || g.opts[1].MaxTokens != budget-first {
				t.Fatalf("budget %d: %+v, %v", budget, g.opts, err)
			}
		}
		if g.opts[0].MaxTokens != first {
			t.Fatalf("first limit = %d", g.opts[0].MaxTokens)
		}
	}
	tool := toolCallTurn("call_1", "echo", `{"q":"a"}`)
	tool.CompletionTokens = 16000
	g := &fakeGateway{limit: 16384, results: []ai.ChatResult{tool, {FinishReason: "stop", Content: "done", CompletionTokens: 384}}}
	_, err := newRunner(g, NewRegistry(&echoTool{name: "echo", read: true}), Config{MaxTotalTokens: 16384}).Run(context.Background(), []ai.Message{{Role: "user", Content: "x"}}, nil)
	if err != nil || g.opts[1].MaxTokens != 384 {
		t.Fatalf("remaining clamp: %+v %v", g.opts, err)
	}
	for _, n := range []int{-1, 0, 32769} {
		if (Config{MaxTotalTokens: n}).withDefaults().MaxTotalTokens != 32768 {
			t.Fatalf("unbounded config %d", n)
		}
	}
}

func TestLongHistoryCompaction(t *testing.T) {
	r := &Runner{config: Config{ContextTokenCap: 100, KeepLastMessages: 2}.withDefaults()}
	history := []ai.Message{{Role: "system", Content: "rules"}, {Role: "user", Content: "old question"}, {Role: "assistant", Content: strings.Repeat("a", 200000)}, {Role: "user", Content: "current question"}, {Role: "assistant", ToolCalls: toolCallTurn("call_1", "echo", `{"q":"a"}`).ToolCalls}, {Role: "tool", ToolCallID: "call_1", Content: "ok"}}
	got := r.compact(history).messages
	if len(got) != 4 || got[0].Content != "rules" || got[1].Content != "current question" || got[2].ToolCalls[0].ID != got[3].ToolCallID {
		t.Fatalf("broken tool round-trip: %+v", got)
	}
	for i, m := range got {
		if i > 0 && m.Role == "system" {
			t.Fatal("injected system digest")
		}
	}
	withoutSystem := r.compact(history[1:]).messages
	if withoutSystem[0].Role != "user" || len(withoutSystem) != 3 {
		t.Fatalf("invalid head: %+v", withoutSystem)
	}
}

func TestRunnerCompactionKeepsHeadAndTail(t *testing.T) {
	runner := &Runner{config: Config{ContextTokenCap: 100, KeepLastMessages: 2}.withDefaults()}
	history := []ai.Message{{Role: "system", Content: "rules"}}
	for i := 0; i < 20; i++ {
		history = append(history, ai.Message{Role: "user", Content: strings.Repeat("x", 100)})
	}
	got := runner.compact(history).messages
	if len(got) > 4 {
		t.Fatalf("compaction kept %d messages", len(got))
	}
	if got[0].Role != "system" || got[0].Content != "rules" {
		t.Fatalf("system prompt lost: %+v", got[0])
	}
	if got[len(got)-1].Content != history[len(history)-1].Content {
		t.Fatalf("tail not preserved")
	}
}

func TestCompactionClearsToolOutputBeforeDroppingTurns(t *testing.T) {
	runner := &Runner{config: Config{ContextTokenCap: 400, KeepLastMessages: 2}.withDefaults()}
	history := []ai.Message{
		{Role: "system", Content: "rules"},
		{Role: "user", Content: "question"},
		{Role: "assistant", ToolCalls: toolCallTurn("call_1", "echo", `{"q":"a"}`).ToolCalls},
		{Role: "tool", ToolCallID: "call_1", Content: strings.Repeat("x", 2000)},
		{Role: "assistant", Content: "answer"},
		{Role: "user", Content: "follow up"},
		{Role: "assistant", Content: "second answer"},
	}
	got := runner.compact(history)
	if got.cleared != 1 || got.dropped != 0 {
		t.Fatalf("cleared=%d dropped=%d, want 1 and 0", got.cleared, got.dropped)
	}
	if len(got.messages) != len(history) {
		t.Fatalf("messages dropped when clearing was enough: %d", len(got.messages))
	}
	if got.messages[3].Content != clearedToolOutput || got.messages[3].ToolCallID != "call_1" {
		t.Fatalf("tool output not cleared in place: %+v", got.messages[3])
	}
	if got.messages[1].Content != "question" {
		t.Fatalf("conversation text was modified: %+v", got.messages[1])
	}
}

func TestEstimateTokensCountsToolSchemas(t *testing.T) {
	tool := &echoTool{name: "echo", read: true}
	registry := NewRegistry(tool)
	history := []ai.Message{{Role: "user", Content: "hi"}}
	without := estimateTokens(history, nil)
	with := estimateTokens(history, registry.Specs())
	if with <= without {
		t.Fatalf("tool schema not counted: without=%d with=%d", without, with)
	}
}

func TestRunnerReportsDuration(t *testing.T) {
	gateway := &fakeGateway{results: []ai.ChatResult{{FinishReason: "stop", Content: "ok", CompletionTokens: 5}}}
	runner := newRunner(gateway, NewRegistry(), Config{})
	base := time.Unix(0, 0)
	runner.now = func() time.Time {
		base = base.Add(25 * time.Millisecond)
		return base
	}
	out, err := runner.Run(context.Background(), []ai.Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.DurationMs <= 0 {
		t.Fatalf("duration not recorded: %+v", out)
	}
}

func TestTruncateMarksCut(t *testing.T) {
	got := truncate("abcdefghij", 4)
	if !strings.HasPrefix(got, "abcd") || !strings.Contains(got, "[truncated") {
		t.Fatalf("truncate = %q", got)
	}
	if truncate("short", 100) != "short" {
		t.Fatalf("short string was modified")
	}
}

func TestRunnerCapsAggregateToolResults(t *testing.T) {
	first := &echoTool{name: "first", read: true, output: strings.Repeat("a", 400)}
	second := &echoTool{name: "second", read: true, output: strings.Repeat("b", 400)}
	gateway := &fakeGateway{results: []ai.ChatResult{
		{
			FinishReason:     "tool_calls",
			CompletionTokens: 10,
			ToolCalls: []ai.ToolCall{
				{ID: "call_1", Type: "function", Function: ai.ToolCallFunction{Name: "first", Arguments: `{"q":"x"}`}},
				{ID: "call_2", Type: "function", Function: ai.ToolCallFunction{Name: "second", Arguments: `{"q":"x"}`}},
			},
		},
		{FinishReason: "stop", Content: "done", CompletionTokens: 5},
	}}
	runner := newRunner(gateway, NewRegistry(first, second), Config{
		ToolResultBytes:    1000,
		MaxToolResultBytes: 500,
	})

	if _, err := runner.Run(context.Background(), []ai.Message{{Role: "user", Content: "hi"}}, func(Event) error {
		return nil
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var toolMessages []ai.Message
	for _, msg := range gateway.last {
		if msg.Role == "tool" {
			toolMessages = append(toolMessages, msg)
		}
	}
	if len(toolMessages) != 2 {
		t.Fatalf("tool messages = %d, want 2", len(toolMessages))
	}
	if !strings.Contains(toolMessages[1].Content, "truncated") {
		t.Fatalf("second result not trimmed: %q", toolMessages[1].Content)
	}
	if len(toolMessages[0].Content)+len(toolMessages[1].Content) > 500+2*len("\n[truncated: result exceeded budget]") {
		t.Fatalf("aggregate tool payload exceeds budget: %d + %d", len(toolMessages[0].Content), len(toolMessages[1].Content))
	}
}

type blockingTool struct{ name string }

func (t *blockingTool) Name() string        { return t.name }
func (t *blockingTool) Description() string { return "blocks until cancelled" }
func (t *blockingTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}},"required":["q"]}`)
}
func (t *blockingTool) ReadOnly() bool { return true }
func (t *blockingTool) Execute(ctx context.Context, _ json.RawMessage) (ToolResult, error) {
	<-ctx.Done()
	return ToolResult{}, ctx.Err()
}

func TestRunnerBoundsToolExecution(t *testing.T) {
	tool := &blockingTool{name: "slow"}
	gateway := &fakeGateway{results: []ai.ChatResult{
		toolCallTurn("call_1", "slow", `{"q":"x"}`),
		{FinishReason: "stop", Content: "done", CompletionTokens: 5},
	}}
	runner := newRunner(gateway, NewRegistry(tool), Config{ToolTimeout: 20 * time.Millisecond})

	out, err := runner.Run(context.Background(), []ai.Message{{Role: "user", Content: "hi"}}, func(Event) error {
		return nil
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(out.Tools) != 1 || out.Tools[0].Err != "tool timed out" {
		t.Fatalf("unexpected tool record: %+v", out.Tools)
	}
}

func TestRunnerResumesInsteadOfBlindRetryAfterStreaming(t *testing.T) {
	gateway := &streamThenFailGateway{}
	runner := newRunner(gateway, NewRegistry(), Config{MaxRetries: 3, MaxPartialContinuations: 1})

	out, err := runner.Run(context.Background(), []ai.Message{{Role: "user", Content: "hi"}}, func(Event) error {
		return nil
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Content != "partial rest" || out.Partial {
		t.Fatalf("out = %+v, want full resumed answer", out)
	}
	// One cut attempt plus one resume attempt: never a blind replay of the
	// same request (which would duplicate the streamed text), but a resume is
	// allowed because it continues from what the user already saw.
	if got := atomic.LoadInt32(&gateway.calls); got != 2 {
		t.Fatalf("provider called %d times after partial output, want 2", got)
	}
}

type streamThenFailGateway struct{ calls int32 }

func (g *streamThenFailGateway) MaxTokens() int { return 512 }

func (g *streamThenFailGateway) ChatStream(_ context.Context, _ []ai.Message, _ ai.ChatOptions, onDelta func(ai.StreamDelta) error) (ai.ChatResult, error) {
	atomic.AddInt32(&g.calls, 1)
	if g.calls == 1 {
		// First attempt is cut mid-answer; the resume attempt completes it.
		if err := onDelta(ai.StreamDelta{Content: "partial"}); err != nil {
			return ai.ChatResult{}, err
		}
		return ai.ChatResult{CompletionTokens: 3}, &ai.ProviderError{Status: 500, Retryable: true}
	}
	if err := onDelta(ai.StreamDelta{Content: " rest"}); err != nil {
		return ai.ChatResult{}, err
	}
	return ai.ChatResult{Content: " rest", FinishReason: "stop", CompletionTokens: 4}, nil
}

func TestRunnerEmitsCompactionEvent(t *testing.T) {
	gateway := &fakeGateway{results: []ai.ChatResult{{FinishReason: "stop", Content: "ok", CompletionTokens: 5}}}
	runner := newRunner(gateway, NewRegistry(), Config{ContextTokenCap: 100, KeepLastMessages: 2})

	history := []ai.Message{{Role: "system", Content: "rules"}}
	for i := 0; i < 20; i++ {
		history = append(history, ai.Message{Role: "user", Content: strings.Repeat("x", 100)})
	}

	var events []string
	if _, err := runner.Run(context.Background(), history, func(e Event) error {
		events = append(events, e.Type)
		return nil
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !slices.Contains(events, "compaction") {
		t.Fatalf("events = %v, want a compaction event", events)
	}
}

func TestRunnerNudgesStalledTurnAfterTools(t *testing.T) {
	tool := &echoTool{name: "echo", read: true, output: "ran:"}
	gateway := &fakeGateway{results: []ai.ChatResult{
		toolCallTurn("call_1", "echo", `{"q":"hi"}`),
		{FinishReason: "stop", Content: "Saya akan mengecek hasilnya.", CompletionTokens: 5},
		{FinishReason: "stop", Content: "Tugas selesai.", CompletionTokens: 5},
	}}
	runner := newRunner(gateway, NewRegistry(tool), Config{MaxContinuationNudges: 8})

	var noticed bool
	out, err := runner.Run(context.Background(), []ai.Message{{Role: "user", Content: "x"}}, func(e Event) error {
		if e.Type == "notice" {
			noticed = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !noticed {
		t.Fatal("expected a continuation notice when the model stalled mid-task")
	}
	// The stall nudge re-prompted the model once, so the gateway was called
	// twice (initial + one nudge) before the conclusive reply closed the turn.
	if gateway.calls != 3 {
		t.Fatalf("calls = %d, want 3 (initial + nudge + conclusive)", gateway.calls)
	}
	if out.Content != "Tugas selesai." {
		t.Fatalf("content = %q", out.Content)
	}
	if atomic.LoadInt32(&tool.calls) != 1 {
		t.Fatalf("tool ran %d times, want 1", tool.calls)
	}
}

func TestRunnerStopsWhenNudgeBudgetExhausted(t *testing.T) {
	tool := &echoTool{name: "echo", read: true, output: "ran:"}
	gateway := &fakeGateway{results: []ai.ChatResult{
		toolCallTurn("call_1", "echo", `{"q":"hi"}`),
		{FinishReason: "stop", Content: "Saya akan mengecek lagi.", CompletionTokens: 5},
		{FinishReason: "stop", Content: "Saya akan mengecek lagi.", CompletionTokens: 5},
	}}
	runner := newRunner(gateway, NewRegistry(tool), Config{MaxContinuationNudges: 1})

	var nudges int
	_, err := runner.Run(context.Background(), []ai.Message{{Role: "user", Content: "x"}}, func(e Event) error {
		if e.Type == "notice" {
			nudges++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if nudges != 1 {
		t.Fatalf("nudges = %d, want 1 (budget of 1 must stop the loop)", nudges)
	}
	if gateway.calls != 3 {
		t.Fatalf("calls = %d, want 3", gateway.calls)
	}
}

func TestRunnerDoesNotNudgeConversationalTurns(t *testing.T) {
	gateway := &fakeGateway{results: []ai.ChatResult{
		{FinishReason: "stop", Content: "Halo, ada yang bisa saya bantu?", CompletionTokens: 5},
	}}
	runner := newRunner(gateway, NewRegistry(), Config{})

	var noticed bool
	if _, err := runner.Run(context.Background(), []ai.Message{{Role: "user", Content: "x"}}, func(e Event) error {
		if e.Type == "notice" {
			noticed = true
		}
		return nil
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if noticed {
		t.Fatal("conversational turns with no tool execution must not be nudged")
	}
}

func TestRunnerForcesToolOnlyOnFirstTurn(t *testing.T) {
	tool := &echoTool{name: "web_search", read: true, output: "found:"}
	gateway := &fakeGateway{results: []ai.ChatResult{
		toolCallTurn("call_1", "web_search", `{"q":"berita terbaru"}`),
		{FinishReason: "stop", Content: "Berikut ringkasannya.", CompletionTokens: 5},
	}}
	runner := newRunner(gateway, NewRegistry(tool), Config{ForceToolCall: "web_search"})

	if _, err := runner.Run(context.Background(), []ai.Message{{Role: "user", Content: "x"}}, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(gateway.opts) != 2 {
		t.Fatalf("calls = %d, want 2", len(gateway.opts))
	}
	if gateway.opts[0].ToolChoice != "web_search" {
		t.Fatalf("first turn tool choice = %q, want web_search", gateway.opts[0].ToolChoice)
	}
	// Forcing on later turns would trap the model in an endless tool loop.
	if gateway.opts[1].ToolChoice != "auto" {
		t.Fatalf("second turn tool choice = %q, want auto", gateway.opts[1].ToolChoice)
	}
}

func TestRunnerForceIgnoredWithoutTools(t *testing.T) {
	gateway := &fakeGateway{results: []ai.ChatResult{
		{FinishReason: "stop", Content: "Halo.", CompletionTokens: 5},
	}}
	runner := newRunner(gateway, NewRegistry(), Config{ForceToolCall: "web_search"})

	if _, err := runner.Run(context.Background(), []ai.Message{{Role: "user", Content: "x"}}, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gateway.opts[0].ToolChoice != "" {
		t.Fatalf("tool choice = %q, want empty when no tools are registered", gateway.opts[0].ToolChoice)
	}
}
