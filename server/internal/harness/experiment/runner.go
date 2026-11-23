package experiment

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"hsg/internal/harness/evidence"
	"hsg/internal/harness/rules"
	"hsg/internal/harness/services"
)

// RunStats is what one arm reports about one item. Cost is nil when the price
// of the model is unknown; the report keeps that absence instead of guessing.
type RunStats struct {
	Latency          time.Duration   `json:"latency_ns,omitempty"`
	PromptTokens     int             `json:"prompt_tokens,omitempty"`
	CompletionTokens int             `json:"completion_tokens,omitempty"`
	Cost             *float64        `json:"cost_usd,omitempty"`
	Evidence         []evidence.Item `json:"evidence,omitempty"`
	RunID            string          `json:"run_id,omitempty"`
	ErrorCode        string          `json:"error_code,omitempty"`
}

// ArmResult is the output an arm produces for one item.
type ArmResult struct {
	Output json.RawMessage
	Stats  RunStats
}

// Arm is one experimental condition. Run must be safe for concurrent calls
// because the runner may invoke it from several goroutines.
type Arm struct {
	Name string
	Run  func(ctx context.Context, input json.RawMessage) (ArmResult, error)
}

// Config controls one run.
type Config struct {
	// Parallel caps concurrent arm×item calls; values below 1 mean 1.
	Parallel int
	// Wrap decorates every arm before it runs. It is where a caller plugs in
	// llm.Replay so an experiment repeats from a recorded fixture. The arm's
	// Name is preserved; only Run is expected to change.
	Wrap func(Arm) Arm
	// RunID names one arm×item run in the run log. The default is
	// deterministic: arm + "#" + item.
	RunID func(arm, item string) string
	// Sink builds the run-log sink for one arm×item. It is attached to the
	// call context and read by the built-in adapters, so every arm×item
	// writes under its own run id.
	Sink func(arm, item, runID string) services.Sink
	// NoWallClock leaves the arm-reported latency untouched instead of timing
	// the call. Replay runs set this so identical recordings produce identical
	// reports.
	NoWallClock bool
}

// ItemResult is the recorded result of one arm on one item.
type ItemResult struct {
	Arm       string
	ItemID    string
	RunID     string
	Output    json.RawMessage
	Stats     RunStats
	Err       string
	ErrorCode string
}

// Results keeps rows grouped by arm in the order the arms were given, then in
// dataset order, so every consumer sees the same layout.
type Results struct {
	ArmNames []string
	Rows     []ItemResult
}

// ForArm returns one arm's rows in dataset order.
func (r Results) ForArm(name string) []ItemResult {
	out := make([]ItemResult, 0)
	for _, row := range r.Rows {
		if row.Arm == name {
			out = append(out, row)
		}
	}
	return out
}

// Execute runs every arm on every item. An arm error is recorded on its row
// and never aborts the run; only a cancelled context or an invalid setup does.
func Execute(ctx context.Context, ds Dataset, arms []Arm, cfg Config) (Results, error) {
	if len(ds.Items) == 0 {
		return Results{}, fmt.Errorf("dataset kosong")
	}
	if len(arms) == 0 {
		return Results{}, fmt.Errorf("tidak ada arm")
	}
	names := make([]string, len(arms))
	seen := map[string]bool{}
	for i := range arms {
		arm := arms[i]
		if arm.Name == "" || arm.Run == nil {
			return Results{}, fmt.Errorf("arm ke-%d tidak lengkap", i)
		}
		if seen[arm.Name] {
			return Results{}, fmt.Errorf("nama arm %q duplikat", arm.Name)
		}
		seen[arm.Name] = true
		names[i] = arm.Name
		if cfg.Wrap != nil {
			wrapped := cfg.Wrap(arm)
			wrapped.Name = arm.Name
			arms[i] = wrapped
		}
	}
	rows := make([]ItemResult, len(arms)*len(ds.Items))
	for ai, name := range names {
		for ii, item := range ds.Items {
			idx := ai*len(ds.Items) + ii
			rows[idx].Arm = name
			rows[idx].ItemID = item.ID
			if cfg.RunID != nil {
				rows[idx].RunID = cfg.RunID(name, item.ID)
			} else {
				rows[idx].RunID = name + "#" + item.ID
			}
		}
	}
	parallel := cfg.Parallel
	if parallel < 1 {
		parallel = 1
	}
	if parallel > len(rows) {
		parallel = len(rows)
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range parallel {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				ai := idx / len(ds.Items)
				item := ds.Items[idx%len(ds.Items)]
				row := rows[idx]
				callCtx := WithRunID(ctx, row.RunID)
				if cfg.Sink != nil {
					callCtx = WithSink(callCtx, cfg.Sink(row.Arm, row.ItemID, row.RunID))
				}
				rows[idx] = runOne(callCtx, arms[ai], row, item.Input, cfg.NoWallClock)
			}
		}()
	}
	for i := range rows {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return Results{}, err
	}
	return Results{ArmNames: names, Rows: rows}, nil
}

func runOne(ctx context.Context, arm Arm, row ItemResult, input json.RawMessage, noWallClock bool) ItemResult {
	started := time.Now()
	result, err := arm.Run(ctx, input)
	elapsed := time.Since(started)
	row.Stats = result.Stats
	if row.Stats.RunID == "" {
		row.Stats.RunID = row.RunID
	}
	// An arm that reports its own latency keeps it; otherwise the wall clock
	// fills the gap. A replay run forbids the wall clock so the report stays
	// byte-identical.
	if row.Stats.Latency == 0 && !noWallClock {
		row.Stats.Latency = elapsed
	}
	row.Output = result.Output
	if err != nil {
		row.Err = err.Error()
		row.ErrorCode = row.Stats.ErrorCode
		if row.ErrorCode == "" {
			row.ErrorCode = "ARM_ERROR"
		}
		return row
	}
	if len(row.Output) > 0 && !json.Valid(row.Output) {
		row.Err = "output arm bukan JSON"
		row.ErrorCode = "OUTPUT_NOT_JSON"
		row.Output = nil
	}
	return row
}

// RulesArm adapts a loaded rule engine to an arm. Output is the engine's hits
// for the requested group; the evidence of fired rules rides on RunStats so
// the report can measure grounding.
func RulesArm(name string, engine *rules.Engine, group string, now func() time.Time) Arm {
	return Arm{Name: name, Run: func(_ context.Context, input json.RawMessage) (ArmResult, error) {
		if engine == nil {
			return ArmResult{}, fmt.Errorf("rule engine nil")
		}
		at := time.Now()
		if now != nil {
			at = now()
		}
		hits := engine.Evaluate(input, group, at)
		out, err := json.Marshal(map[string]any{"group": group, "hits": hits})
		if err != nil {
			return ArmResult{}, err
		}
		var items []evidence.Item
		for _, hit := range hits {
			if hit.Fired {
				items = append(items, hit.Evidence...)
			}
		}
		return ArmResult{Output: out, Stats: RunStats{Evidence: items}}, nil
	}}
}

// RunnerArm adapts an external runner (a service or a caller's own agent) to
// an arm. Use ProfileArm or TeamArm for the harness's own run paths.
func RunnerArm(name string, runner ArmRunner) Arm {
	return Arm{Name: name, Run: func(ctx context.Context, input json.RawMessage) (ArmResult, error) {
		output, stats, err := runner.Run(ctx, input)
		if stats.RunID == "" {
			stats.RunID = RunIDFrom(ctx)
		}
		return ArmResult{Output: output, Stats: stats}, err
	}}
}

// ArmRunner is the narrow view an external runner must expose to become an
// arm.
type ArmRunner interface {
	Run(ctx context.Context, input json.RawMessage) (json.RawMessage, RunStats, error)
}
