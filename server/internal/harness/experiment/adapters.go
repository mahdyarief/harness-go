package experiment

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"hsg/internal/harness/agentloop"
	"hsg/internal/harness/cost"
	"hsg/internal/harness/profile"
	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
	"hsg/internal/harness/team"
	"hsg/internal/lib/ai"
)

// ProfileArmConfig wires one profile to the real harness loop. Parent must
// already carry an LLM and the agent registry (see mount.Mount); pass an
// llm.Replay-backed LLM to make the arm reproducible.
type ProfileArmConfig struct {
	Parent  *scope.Scope
	Profile profile.Profile
	// Model and Cost price the usage; both are optional. Without a price the
	// stats carry a nil cost instead of guessing.
	Model   string
	Cost    *cost.Table
	Timeout time.Duration
}

// ProfileArm runs the production loop for one profile, measuring the same
// path a chat uses instead of a lookalike.
func ProfileArm(name string, cfg ProfileArmConfig) (Arm, error) {
	if cfg.Parent == nil {
		return Arm{}, fmt.Errorf("profile arm %q: parent scope wajib", name)
	}
	return Arm{Name: name, Run: func(ctx context.Context, input json.RawMessage) (ArmResult, error) {
		if cfg.Timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, cfg.Timeout)
			defer cancel()
		}
		child, err := profile.MountProfile(cfg.Parent, cfg.Profile)
		if err != nil {
			return ArmResult{Stats: RunStats{RunID: RunIDFrom(ctx)}}, err
		}
		out, err := agentloop.New(child).Run(ctx, services.RunRequest{
			Messages:   []ai.Message{{Role: "user", Content: string(input)}},
			SystemText: cfg.Profile.System,
		}, SinkFrom(ctx))
		stats := RunStats{
			RunID:            RunIDFrom(ctx),
			PromptTokens:     out.PromptTokens,
			CompletionTokens: out.CompletionTokens,
		}
		if cfg.Cost != nil && cfg.Model != "" {
			if amount, costErr := cfg.Cost.Cost(cfg.Model, cost.Usage{
				PromptTokens:     out.PromptTokens,
				CompletionTokens: out.CompletionTokens,
			}); costErr == nil {
				value := amount.USD
				stats.Cost = &value
			}
		}
		output := out.Structured
		if len(output) == 0 {
			output, _ = json.Marshal(map[string]any{"content": out.Content})
		}
		return ArmResult{Output: output, Stats: stats}, err
	}}, nil
}

// TeamArmConfig wires a team and its reducer to one arm.
type TeamArmConfig struct {
	Parent  *scope.Scope
	Members []team.Member
	Config  team.Config
	Reducer team.Rules[json.RawMessage]
}

// TeamArm runs the team for each item and reduces the members to one output.
// Each member's user message is replaced with the item input; its system
// prompt still comes from the profile.
func TeamArm(name string, cfg TeamArmConfig) Arm {
	return Arm{Name: name, Run: func(ctx context.Context, input json.RawMessage) (ArmResult, error) {
		members := make([]team.Member, len(cfg.Members))
		copy(members, cfg.Members)
		for i := range members {
			members[i].Input.Messages = []ai.Message{{Role: "user", Content: string(input)}}
		}
		result, err := team.Run(ctx, cfg.Parent, members, cfg.Config, SinkFrom(ctx))
		stats := RunStats{
			RunID:            RunIDFrom(ctx),
			PromptTokens:     result.PromptTokens,
			CompletionTokens: result.CompletionTokens,
			Cost:             result.CostUSD,
		}
		if err != nil {
			return ArmResult{Stats: stats}, err
		}
		value, _, ok := cfg.Reducer.Reduce(result.Members)
		if !ok {
			return ArmResult{Stats: stats}, fmt.Errorf("team %q: tidak ada hasil konsensus", name)
		}
		return ArmResult{Output: value, Stats: stats}, nil
	}}
}
