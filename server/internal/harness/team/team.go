package team

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"hsg/internal/harness/agentloop"
	"hsg/internal/harness/profile"
	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
)

type Member struct {
	Profile profile.Profile
	Input   services.RunRequest
}

type MemberResult struct {
	Name    string
	Outcome services.Outcome
	Err     error
	ErrCode string
}

type Config struct {
	MaxParallel   int
	MemberTimeout time.Duration
}

type Result struct {
	Members          []MemberResult
	PromptTokens     int
	CompletionTokens int
	Duration         time.Duration
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
			return aggregate(results, time.Since(started)), nil
		}
	}
	close(jobs)
	wg.Wait()
	return aggregate(results, time.Since(started)), nil
}

func runMember(parentCtx context.Context, parent *scope.Scope, member Member, timeout time.Duration, sink services.Sink) MemberResult {
	result := MemberResult{Name: member.Profile.Name}
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

func aggregate(results []MemberResult, duration time.Duration) Result {
	total := Result{Members: results, Duration: duration}
	for _, result := range results {
		total.PromptTokens += result.Outcome.PromptTokens
		total.CompletionTokens += result.Outcome.CompletionTokens
	}
	return total
}
