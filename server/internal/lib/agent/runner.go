package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"hsg/internal/lib/ai"
)

// Config bounds one agent run. Zero values fall back to the defaults below so a
// caller cannot accidentally run unbounded.
type Config struct {
	MaxTurns        int
	MaxToolCalls    int
	MaxDuration     time.Duration
	MaxTotalTokens  int
	ToolResultBytes int
	// MaxToolResultBytes bounds the combined tool output fed back in one turn.
	// Per-tool truncation alone still lets several large results crowd the
	// next request, so the aggregate is capped in model order.
	MaxToolResultBytes int
	MaxToolConcurrency int
	ToolTimeout        time.Duration
	ContextTokenCap    int
	KeepLastMessages   int
	MaxRetries         int
	// MaxContinuationNudges bounds how many times the loop may re-prompt the
	// model after it stops with tools already executed but no clear conclusion,
	// so a stalling model cannot loop forever.
	MaxContinuationNudges int
	// MaxPartialContinuations bounds how many times a cut stream is resumed
	// from the text already delivered, so a flaky provider cannot loop forever.
	MaxPartialContinuations int
	// ForceToolCall names a tool the first model call must invoke. It is used by
	// an explicit user "use web search" toggle, and only on the first turn so
	// the model can still answer normally after the tool result. An empty value
	// leaves tool choice on "auto".
	ForceToolCall string
	// LoopGuards are consulted after every turn's tools have run and may end the
	// run early. Empty by default, which leaves the loop's behavior unchanged.
	LoopGuards []LoopGuard
}

func (c Config) withDefaults() Config {
	if c.MaxTurns <= 0 || c.MaxTurns > 8 {
		c.MaxTurns = 6
	}
	if c.MaxToolCalls <= 0 || c.MaxToolCalls > 16 {
		c.MaxToolCalls = 8
	}
	if c.MaxDuration <= 0 || c.MaxDuration > 2*time.Minute {
		c.MaxDuration = 90 * time.Second
	}
	if c.MaxTotalTokens <= 0 || c.MaxTotalTokens > 32768 {
		c.MaxTotalTokens = 32768
	}
	if c.ToolResultBytes <= 0 || c.ToolResultBytes > 32768 {
		c.ToolResultBytes = 8192
	}
	if c.MaxToolResultBytes <= 0 || c.MaxToolResultBytes > 65536 {
		c.MaxToolResultBytes = 24576
	}
	if c.MaxToolConcurrency <= 0 || c.MaxToolConcurrency > 8 {
		c.MaxToolConcurrency = 4
	}
	if c.ToolTimeout <= 0 || c.ToolTimeout > 30*time.Second {
		c.ToolTimeout = 15 * time.Second
	}
	if c.ContextTokenCap <= 0 || c.ContextTokenCap > 32768 {
		c.ContextTokenCap = 12288
	}
	if c.KeepLastMessages <= 0 || c.KeepLastMessages > 32 {
		c.KeepLastMessages = 12
	}
	if c.MaxRetries < 0 || c.MaxRetries > 3 {
		c.MaxRetries = 1
	}
	if c.MaxContinuationNudges < 0 || c.MaxContinuationNudges > 20 {
		c.MaxContinuationNudges = 8
	}
	if c.MaxPartialContinuations <= 0 || c.MaxPartialContinuations > 8 {
		c.MaxPartialContinuations = 2
	}
	return c
}

// Event is what a streaming caller renders. Text deltas arrive in order; tool
// events bracket each invocation so the UI can show progress.
// The tags are the wire format for the SSE frames the web client parses.
type Event struct {
	Type       string `json:"type"`
	Text       string `json:"text,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	ToolCall   string `json:"tool_call,omitempty"`
	Error      string `json:"error,omitempty"`
	DurationMs int    `json:"duration_ms,omitempty"`
}

type Sink func(Event) error

// chatGateway is the part of *ai.Gateway the harness uses. Narrowing it keeps
// the loop testable without opening a network connection.
type chatGateway interface {
	MaxTokens() int
	ChatStream(ctx context.Context, messages []ai.Message, opts ai.ChatOptions, onDelta func(ai.StreamDelta) error) (ai.ChatResult, error)
}

// Runner executes the bounded tool loop over one gateway.
type Runner struct {
	gateway  chatGateway
	registry *Registry
	config   Config
	now      func() time.Time
}

func NewRunner(gateway *ai.Gateway, registry *Registry, config Config) *Runner {
	return newRunner(gateway, registry, config)
}

func newRunner(gateway chatGateway, registry *Registry, config Config) *Runner {
	return &Runner{gateway: gateway, registry: registry, config: config.withDefaults(), now: time.Now}
}

// Outcome is the terminal state of a run. Content is the model's final text;
// Err is nil only when the model stopped on its own within budget.
type Outcome struct {
	Content          string
	PromptTokens     int
	CompletionTokens int
	Turns            int
	ToolCalls        int
	Tools            []ToolRecord
	// References is the deduplicated union of every tool's provenance records,
	// in first-seen order. A tool that cites nothing contributes nothing.
	References []Reference
	// DurationMs is the wall-clock time of the whole run, including every
	// provider call and tool invocation. Callers export it as a per-turn metric.
	DurationMs int
	// Partial marks Content as text that was streamed before the run failed.
	// It is saved for the user but flagged so the transcript can show the turn
	// did not finish.
	Partial bool
}

// Run streams one agent turn. It never panics on provider content: every
// failure becomes an error the caller can map to a safe message.
func (r *Runner) Run(ctx context.Context, messages []ai.Message, sink Sink) (out Outcome, err error) {
	if r == nil || r.gateway == nil {
		return out, ai.ErrConfig
	}
	start := r.now()
	defer func() { out.DurationMs = int(r.now().Sub(start).Milliseconds()) }()
	if sink == nil {
		sink = func(Event) error { return nil }
	}
	// outerCtx adalah konteks pemanggil (request Fiber): satu-satunya sinyal
	// yang sejati untuk "klien membatalkan" (AbortError). Batas durasi run
	// tidak lagi membungkus seluruh loop dengan satu deadline, supaya sebuah
	// jawaban yang terputus oleh batas waktu provider masih bisa dilanjutkan
	// sebagai percobaan baru, bukan dibatalkan lalu disimpan sebagai partial.
	// Setiap panggilan provider memakai batas waktunya sendiri (gateway.Timeout),
	// dan budget token total run mengendalikan durasi keseluruhan.
	outerCtx := ctx
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	history := append([]ai.Message(nil), messages...)
	toolCalls := 0
	nudges := 0
	partialContinuations := 0
	// carried holds text streamed by a cut attempt that the next turn resumes
	// from, so the persisted answer is the whole reply rather than its tail.
	var carried strings.Builder
	// Guard memory lives for this run only, so guards stay stateless and shared.
	guardMemory := make([]Memory, len(r.config.LoopGuards))
	for i := range guardMemory {
		guardMemory[i] = Memory{}
	}
	for turn := 0; turn < r.config.MaxTurns; turn++ {
		out.Turns = turn + 1
		compacted := r.compact(history)
		if compacted.cleared > 0 || compacted.dropped > 0 {
			// Surface the drop so the UI can say why earlier context vanished
			// instead of the model silently losing the thread.
			if err := sink(Event{Type: "compaction", Text: compacted.describe()}); err != nil {
				return out, err
			}
		}
		history = compacted.messages

		remaining := r.config.MaxTotalTokens - out.CompletionTokens
		if remaining <= 0 {
			_ = sink(Event{Type: "budget", Error: "token budget exhausted"})
			return out, fmt.Errorf("token budget exhausted: %w", ai.ErrOutput)
		}
		forced := ""
		if turn == 0 {
			forced = r.config.ForceToolCall
		}
		result, err := r.call(ctx, history, remaining, sink, forced)
		out.PromptTokens += result.PromptTokens
		out.CompletionTokens += result.CompletionTokens
		if err != nil {
			// A stream cut (client abort, proxy timeout, provider drop) leaves
			// text that the user already saw on screen. Resume it so the turn
			// ends with the complete answer instead of a partial one. Only
			// text-bearing cuts are worth resuming; a hard provider refusal or
			// exhaustion ends the turn as before. The budget prevents loops.
			// Only a provider-side cut is resumable. A failed sink means the
			// client is gone, and a size/budget rejection must not be retried.
			cut := result.Content != "" && out.Content == "" &&
				(errors.Is(err, ai.ErrOutput) || errors.Is(err, ai.ErrProvider))
			// Cek abort klien, bukan deadline percobaan: ctx di atas tidak
			// pernah habis (deadline dipindahkan ke gateway), jadi resume
			// selalu diizinkan bila klien masih terhubung.
			if cut && partialContinuations < r.config.MaxPartialContinuations && outerCtx.Err() == nil && out.CompletionTokens <= r.config.MaxTotalTokens {
				partialContinuations++
				carried.WriteString(result.Content)
				history = append(history, ai.Message{Role: "assistant", Content: result.Content})
				history = append(history, ai.Message{
					Role:    "user",
					Content: "Lanjutkan jawaban dari titik terakhir tanpa mengulang. Selesaikan hingga selesai.",
				})
				if err := sink(Event{
					Type: "notice",
					Text: "Jawaban terputus; melanjutkan dari bagian terakhir.",
				}); err != nil {
					return out, err
				}
				continue
			}
			if out.Content == "" && result.Content != "" {
				out.Content = carried.String() + result.Content
				out.Partial = true
			}
			return out, err
		}
		if out.CompletionTokens > r.config.MaxTotalTokens {
			_ = sink(Event{Type: "budget", Error: "token budget exhausted"})
			return out, fmt.Errorf("token budget exhausted: %w", ai.ErrOutput)
		}
		if result.FinishReason != "tool_calls" {
			// The model stopped with text. If it already ran tools this turn but
			// its closing text does not conclude the task (it stalled mid-thought,
			// returned empty, or signalled more work), re-prompt it to finish
			// rather than ending the turn prematurely. This mirrors the anti-stall
			// loop of the OpenClaude CLI. Conversational turns (no tools) are left
			// alone, and the nudge budget prevents an infinite loop.
			if toolCalls > 0 && nudges < r.config.MaxContinuationNudges &&
				shouldContinue(result.Content, nudges > 0) {
				if nudges == 0 {
					_ = sink(Event{
						Type: "notice",
						Text: "Model berhenti setelah menggunakan alat tanpa menyelesaikan tugas. Mendorong untuk melanjutkan.",
					})
				}
				nudges++
				history = append(history, ai.Message{
					Role:    "user",
					Content: "Lanjutkan tugas dengan alat yang tersedia. Setelah benar-benar selesai, berikan ringkasan akhir yang singkat. Jika menunggu keputusan pengguna, ajukan pertanyaan.",
				})
				continue
			}
			out.Content = carried.String() + result.Content
			return out, nil
		}
		if len(result.ToolCalls) == 0 {
			return out, ai.ErrOutput
		}

		history = append(history, ai.Message{Role: "assistant", ToolCalls: result.ToolCalls})
		results, toolRecords, err := r.dispatch(ctx, turn, result.ToolCalls, sink, &toolCalls)
		if err != nil {
			return out, err
		}
		for _, tr := range toolRecords {
			if tr.Name != "" {
				out.Tools = append(out.Tools, tr)
			}
			for _, ref := range tr.References {
				out.References = appendUniqueReference(out.References, ref)
			}
		}
		history = append(history, results...)
		out.ToolCalls = toolCalls
		if toolCalls > r.config.MaxToolCalls {
			_ = sink(Event{Type: "budget", Error: "tool budget exhausted"})
			return out, fmt.Errorf("tool budget exhausted: %w", ai.ErrOutput)
		}
		// A guard stop is a graceful end: its reason is the answer, so the
		// caller persists and renders it like any other conclusion.
		if verdict, guard, stop := inspect(r.config.LoopGuards, guardMemory, LoopTurn{Turn: turn, Records: toolRecords, History: out.Tools}); stop {
			// Only the guard's name and run counters are logged: arguments and
			// tool output can carry user or tenant data.
			slog.Warn("agent loop guard stopped run", "guard", guard, "turn", turn, "tool_calls", toolCalls)
			if err := sink(Event{Type: "delta", Text: verdict.Reason}); err != nil {
				return out, err
			}
			out.Content = verdict.Reason
			return out, nil
		}
	}
	_ = sink(Event{Type: "budget", Error: "turn budget exhausted"})
	return out, fmt.Errorf("turn budget exhausted: %w", ai.ErrOutput)
}

func (r *Runner) call(ctx context.Context, history []ai.Message, remaining int, sink Sink, force string) (ai.ChatResult, error) {
	limit := min(r.gateway.MaxTokens(), ai.MaxOutputTokens, remaining)
	if limit <= 0 {
		return ai.ChatResult{}, ai.ErrOutput
	}
	choice := "auto"
	if force != "" {
		choice = force
	}
	opts := ai.ChatOptions{MaxTokens: limit, Tools: r.registry.Specs(), ToolChoice: choice}
	if len(opts.Tools) == 0 {
		opts.ToolChoice = ""
	}
	var lastErr error
	var usage ai.ChatResult
	// Streamed text is captured so a failed/disconnected stream still yields the
	// answer the user already saw, for persistence and citation metering.
	var streamed strings.Builder
	for attempt := 0; attempt <= r.config.MaxRetries; attempt++ {
		// A retry replays the request, so any text already streamed would be
		// rendered twice. Once a delta is out, the attempt is final.
		emitted := false
		streamed.Reset()
		result, err := r.gateway.ChatStream(ctx, history, opts, func(d ai.StreamDelta) error {
			if d.Content == "" {
				return nil
			}
			emitted = true
			streamed.WriteString(d.Content)
			return sink(Event{Type: "delta", Text: d.Content})
		})
		// Count each attempt exactly once, including results returned with errors.
		usage.PromptTokens += max(0, result.PromptTokens)
		usage.CompletionTokens += max(0, result.CompletionTokens)
		if err == nil {
			if result.PromptTokens < 0 || result.CompletionTokens < 0 || result.CompletionTokens > limit {
				return usage, ai.ErrOutput
			}
			result.PromptTokens = usage.PromptTokens
			result.CompletionTokens = usage.CompletionTokens
			return result, nil
		}
		lastErr = err
		var providerErr *ai.ProviderError
		if !errors.As(err, &providerErr) || !providerErr.Retryable || ctx.Err() != nil || emitted {
			// Preserve the captured text and any real finish reason so the
			// caller can resume a cut stream from exactly where it stopped.
			usage.Content = streamed.String()
			if result.FinishReason != "" {
				usage.FinishReason = result.FinishReason
			}
			// A stream that ended without [DONE] is a truncated response, not a
			// content-rejection: report ErrOutput so the caller can resume it.
			if errors.Is(err, ai.ErrOutput) {
				return usage, ai.ErrOutput
			}
			return usage, err
		}
		if attempt == r.config.MaxRetries {
			break
		}
		if err := sleepContext(ctx, time.Duration(attempt+1)*150*time.Millisecond); err != nil {
			usage.Content = streamed.String()
			return usage, err
		}
	}
	usage.Content = streamed.String()
	return usage, lastErr
}

// ToolRecord is an audit row for one invocation. It is produced here so the
// caller never has to reconstruct what ran.
type ToolRecord struct {
	Turn       int
	Name       string
	Arguments  string
	Result     string
	DurationMs int
	Err        string
	// References are provenance records the tool returned alongside its output.
	// They are not part of the tool message the model reads.
	References []Reference
}

// dispatch runs read-only tools concurrently and write tools one at a time.
// Results are returned in the model's original order regardless of finish order.
func (r *Runner) dispatch(ctx context.Context, turn int, calls []ai.ToolCall, sink Sink, counter *int) ([]ai.Message, []ToolRecord, error) {
	results := make([]ai.Message, len(calls))
	records := make([]ToolRecord, len(calls))
	parallel := make([]bool, len(calls))

	type job struct {
		index int
		tool  Tool
		args  json.RawMessage
	}
	var jobs []job
	for i, call := range calls {
		tool, ok := r.registry.Lookup(call.Function.Name)
		if !ok {
			results[i] = toolError(call, "tool is not available")
			records[i] = ToolRecord{Turn: turn, Name: call.Function.Name, Arguments: call.Function.Arguments, Err: "tool not in allow-list"}
			continue
		}
		args := json.RawMessage(call.Function.Arguments)
		if err := validateArgs(tool.Parameters(), args); err != nil {
			results[i] = toolError(call, "tool arguments do not match the schema")
			records[i] = ToolRecord{Turn: turn, Name: tool.Name(), Arguments: call.Function.Arguments, Err: "arguments do not match schema"}
			continue
		}
		jobs = append(jobs, job{index: i, tool: tool, args: args})
		parallel[i] = tool.ReadOnly()
	}
	if len(jobs) == 0 {
		return results, records, nil
	}
	*counter += len(jobs)

	// tool_start is emitted up front, in model order, so the stream stays
	// deterministic even when the read-only calls finish out of order.
	for _, j := range jobs {
		if err := sink(Event{Type: "tool_start", ToolName: j.tool.Name(), ToolCall: calls[j.index].ID}); err != nil {
			return nil, nil, err
		}
	}

	runJob := func(j job) {
		start := r.now()
		// A tool that ignores cancellation must not hold the turn open past
		// the run deadline, so each invocation gets its own bounded window.
		toolCtx, cancel := context.WithTimeout(ctx, r.config.ToolTimeout)
		res, err := j.tool.Execute(toolCtx, j.args)
		cancel()
		elapsed := int(r.now().Sub(start).Milliseconds())
		record := ToolRecord{Turn: turn, Name: j.tool.Name(), Arguments: calls[j.index].Function.Arguments, DurationMs: elapsed}
		if err != nil {
			message := "tool execution failed"
			var rejection *GuardRejection
			switch {
			case errors.As(err, &rejection):
				// The guard's reason is written for the model so it can correct
				// the call; the record keeps only which guard fired.
				results[j.index] = toolError(calls[j.index], rejection.Reason)
				record.Err = "rejected by guard " + rejection.Guard
				records[j.index] = record
				return
			case errors.Is(err, context.DeadlineExceeded):
				message = "tool timed out"
			}
			results[j.index] = toolError(calls[j.index], message)
			record.Err = message
			records[j.index] = record
			return
		}
		content := truncate(res.Output, r.config.ToolResultBytes)
		results[j.index] = ai.Message{Role: "tool", ToolCallID: calls[j.index].ID, Content: content}
		record.Result = content
		record.References = res.References
		records[j.index] = record
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, r.config.MaxToolConcurrency)
	for _, j := range jobs {
		if !parallel[j.index] {
			continue
		}
		wg.Add(1)
		go func(j job) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			runJob(j)
		}(j)
	}
	wg.Wait()
	for _, j := range jobs {
		if parallel[j.index] {
			continue
		}
		runJob(j)
	}

	// Cap the combined tool payload in model order. Per-call truncation keeps
	// one result bounded, but several large results would still crowd out the
	// conversation on the next request.
	budget := r.config.MaxToolResultBytes
	for _, j := range jobs {
		msg := &results[j.index]
		if msg.Content == "" {
			continue
		}
		if budget <= 0 {
			msg.Content = "[omitted: aggregate tool result budget exhausted]"
			records[j.index].Result = msg.Content
			continue
		}
		if len(msg.Content) > budget {
			msg.Content = truncate(msg.Content, budget)
			records[j.index].Result = msg.Content
		}
		budget -= len(msg.Content)
	}
	for _, j := range jobs {
		event := Event{Type: "tool_end", ToolName: j.tool.Name(), ToolCall: calls[j.index].ID, DurationMs: records[j.index].DurationMs}
		if records[j.index].Err != "" {
			event.Error = records[j.index].Err
		}
		if err := sink(event); err != nil {
			return nil, nil, err
		}
	}
	return results, records, nil
}

func toolError(call ai.ToolCall, message string) ai.Message {
	payload, _ := json.Marshal(map[string]string{"error": message})
	return ai.Message{Role: "tool", ToolCallID: call.ID, Content: string(payload)}
}

// clearedToolOutput marks a tool result whose body was dropped to reclaim
// context. The tool_call_id stays so the transcript remains well formed.
const clearedToolOutput = "[output tool lama dikosongkan untuk menghemat konteks]"

// compactResult reports what compaction actually did, so the caller can tell
// the user why the model lost part of the thread.
type compactResult struct {
	messages []ai.Message
	cleared  int
	dropped  int
}

func (c compactResult) describe() string {
	switch {
	case c.cleared > 0 && c.dropped > 0:
		return fmt.Sprintf("%d output tool dikosongkan, %d pesan dilewati", c.cleared, c.dropped)
	case c.cleared > 0:
		return fmt.Sprintf("%d output tool lama dikosongkan", c.cleared)
	default:
		return fmt.Sprintf("%d pesan dilewati", c.dropped)
	}
}

// compact reclaims context in two tiers. Clearing old tool output is the
// lowest-risk move: the model keeps the conversation but loses bulky payloads
// it can request again. Dropping whole user turns is the fallback, and it never
// introduces a system message or orphans a tool response. The current
// user/tool round-trip stays intact; oversized single turns are still rejected
// by the gateway's hard byte caps.
func (r *Runner) compact(history []ai.Message) compactResult {
	specs := r.registry.Specs()
	if estimateTokens(history, specs) <= r.config.ContextTokenCap {
		return compactResult{messages: history}
	}

	head := 0
	if len(history) > 0 && history[0].Role == "system" {
		head = 1
	}

	// Tier one: blank tool bodies outside the protected tail window.
	keepFrom := max(head, len(history)-r.config.KeepLastMessages)
	cleared := append([]ai.Message(nil), history...)
	clearedCount := 0
	for i := head; i < keepFrom; i++ {
		if cleared[i].Role != "tool" || cleared[i].Content == "" || cleared[i].Content == clearedToolOutput {
			continue
		}
		cleared[i].Content = clearedToolOutput
		clearedCount++
	}
	if clearedCount > 0 {
		if estimateTokens(cleared, specs) <= r.config.ContextTokenCap {
			return compactResult{messages: cleared, cleared: clearedCount}
		}
	}

	// Tier two: drop whole oldest user turns from the cleared history.
	start := keepFrom
	for start > head && cleared[start].Role != "user" {
		start--
	}
	for {
		compacted := append([]ai.Message(nil), cleared[:head]...)
		compacted = append(compacted, cleared[start:]...)
		if estimateTokens(compacted, specs) <= r.config.ContextTokenCap {
			return compactResult{messages: compacted, cleared: clearedCount, dropped: len(history) - len(compacted)}
		}
		next := start + 1
		for next < len(cleared) && cleared[next].Role != "user" {
			next++
		}
		if next >= len(cleared) {
			return compactResult{messages: compacted, cleared: clearedCount, dropped: len(history) - len(compacted)}
		}
		start = next
	}
}

// estimateTokens is deliberately conservative: roughly four bytes per token.
// Tool schemas are sent on every request, so their size counts against the same
// context budget the history is measured with.
func estimateTokens(messages []ai.Message, specs []ai.ToolSpec) int {
	total := 0
	for _, m := range messages {
		total += len(m.Content)/4 + 4
		for _, call := range m.ToolCalls {
			total += len(call.Function.Arguments)/4 + 8
		}
	}
	for _, spec := range specs {
		total += len(spec.Function.Name)/4 + len(spec.Function.Description)/4 + len(spec.Function.Parameters)/4 + 12
	}
	return total
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// appendUniqueReference keeps the first occurrence of a chunk. Two searches in
// one turn commonly return the same passage; the answer should list it once.
func appendUniqueReference(refs []Reference, ref Reference) []Reference {
	for _, existing := range refs {
		if existing.ChunkID == ref.ChunkID {
			return refs
		}
	}
	return append(refs, ref)
}
