package team

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"hsg/internal/harness/agentloop"
	"hsg/internal/harness/cost"
	"hsg/internal/harness/profile"
	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
)

type Member struct {
	// Model is the priced model this member runs on. It is separate from the
	// profile because a profile describes behaviour, not deployment.
	Model   string
	Profile profile.Profile
	Input   services.RunRequest
}

type MemberResult struct {
	Name    string
	Model   string
	Outcome services.Outcome
	Err     error
	ErrCode string
	// Duration is the member's own wall time, including a timeout.
	Duration time.Duration
	// CostUSD is nil when the member had no priced model, so an unpriced run
	// never looks like a free one.
	CostUSD *float64
}

type Config struct {
	MaxParallel   int
	MemberTimeout time.Duration
	// Cost prices member usage. A nil table disables accounting and leaves
	// every cost nil.
	Cost *cost.Table
	// ReduceName names the reducer rule that won, and optionally the reduced
	// value. When set, Run emits reduce/decision so a replay can rebuild the
	// same final result and the audit log records why.
	ReduceName func(Result) (rule string, value json.RawMessage, ok bool)
}

type Result struct {
	Members          []MemberResult
	PromptTokens     int
	CompletionTokens int
	// CostUSD is the sum of priced members, or nil when none could be priced.
	CostUSD *float64
	// Warnings lists members that could not be priced. They never fail the run.
	Warnings []string
	Duration time.Duration
}

func Run(ctx context.Context, parent *scope.Scope, members []Member, cfg Config, sink services.Sink) (Result, error) {
	if cfg.MaxParallel < 1 || cfg.MemberTimeout <= 0 {
		return Result{}, fmt.Errorf("invalid team config")
	}
	results := make([]MemberResult, len(members))
	if len(members) == 0 {
		return Result{Members: results}, nil
	}
	started := time.Now()
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range min(cfg.MaxParallel, len(members)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				results[i] = runMember(ctx, parent, members[i], cfg.MemberTimeout, sink)
			}
		}()
	}
	for i := range members {
		select {
		case jobs <- i:
		case <-ctx.Done():
			for j := i; j < len(members); j++ {
				results[j] = MemberResult{Name: members[j].Profile.Name, Err: ctx.Err(), ErrCode: "AGENT_FAILED"}
			}
			close(jobs)
			wg.Wait()
			total := aggregate(results, time.Since(started), cfg.Cost)
			emitDecision(sink, cfg, total)
			return total, nil
		}
	}
	close(jobs)
	wg.Wait()
	total := aggregate(results, time.Since(started), cfg.Cost)
	emitDecision(sink, cfg, total)
	return total, nil
}

// emitDecision records the winning reducer rule. It is best effort: a sink
// error must never change the run's outcome.
func emitDecision(sink services.Sink, cfg Config, result Result) {
	if sink == nil || cfg.ReduceName == nil {
		return
	}
	rule, value, ok := cfg.ReduceName(result)
	if !ok {
		return
	}
	payload, err := json.Marshal(map[string]any{
		"rule": rule, "matched": true, "value": value,
	})
	if err != nil {
		return
	}
	_ = sink(services.AIEvent{Type: "reduce/decision", Payload: payload})
}

func runMember(parentCtx context.Context, parent *scope.Scope, member Member, timeout time.Duration, sink services.Sink) (result MemberResult) {
	started := time.Now()
	defer func() { result.Duration = time.Since(started) }()
	result = MemberResult{Name: member.Profile.Name, Model: member.Model}
	ctx, cancel := context.WithTimeout(parentCtx, timeout)
	defer cancel()
	child, err := profile.MountProfile(parent, member.Profile)
	if err != nil {
		result.Err, result.ErrCode = err, "AGENT_FAILED"
		return result
	}
	input := member.Input
	if input.SystemText == "" {
		input.SystemText = member.Profile.System
	} else if member.Profile.System != "" {
		input.SystemText = member.Profile.System + "\n\n" + input.SystemText
	}
	out, err := agentloop.New(child).Run(ctx, input, func(event services.AIEvent) error {
		if sink == nil {
			return nil
		}
		event.Agent = member.Profile.Name
		return sink(event)
	})
	result.Outcome = out
	if err != nil {
		result.Err = err
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			result.ErrCode = "AGENT_TIMEOUT"
		case errors.Is(ctx.Err(), context.Canceled):
			result.ErrCode = "AGENT_FAILED"
		case services.ErrorCode(err) != "":
			result.ErrCode = services.ErrorCode(err)
		default:
			result.ErrCode = "AGENT_FAILED"
		}
	}
	return result
}

// aggregate sums usage and prices each member. Outcome usage is already the
// billed total for the member's run (the loop adds every attempt, including
// failed ones, at call time), so pricing the outcome prices what was billed.
func aggregate(results []MemberResult, duration time.Duration, table *cost.Table) Result {
	total := Result{Members: results, Duration: duration}
	for _, result := range results {
		total.PromptTokens += result.Outcome.PromptTokens
		total.CompletionTokens += result.Outcome.CompletionTokens
	}
	if table == nil {
		return total
	}
	var sum float64
	priced := 0
	for i := range total.Members {
		member := &total.Members[i]
		if member.Model == "" {
			total.Warnings = append(total.Warnings, fmt.Sprintf("member %q tidak memiliki model; biaya tidak dihitung", member.Name))
			continue
		}
		amount, err := table.Cost(member.Model, cost.Usage{
			PromptTokens:     member.Outcome.PromptTokens,
			CompletionTokens: member.Outcome.CompletionTokens,
		})
		if err != nil {
			total.Warnings = append(total.Warnings, err.Error())
			continue
		}
		value := amount.USD
		member.CostUSD = &value
		sum += value
		priced++
	}
	if priced > 0 {
		total.CostUSD = &sum
	}
	return total
}
