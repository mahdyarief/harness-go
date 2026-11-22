package team

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"hsg/internal/harness/profile"
	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
	"hsg/internal/lib/agent"
	"hsg/internal/lib/ai"
)

// ExampleRun is the Task 4 checkpoint in a neutral domain (letter
// frequency): three profiles run in parallel, each submits a structured
// result, and an ordered rule reducer combines them.
func ExampleRun() {
	frequencies := map[string]int{"a": 3, "b": 4, "c": 2}
	llm := &memberLLM{fn: func(_ context.Context, messages []ai.Message, _ ai.ChatOptions, _ func(ai.StreamDelta) error) (ai.ChatResult, error) {
		args, _ := json.Marshal(map[string]int{"value": frequencies[messages[len(messages)-1].Content]})
		return ai.ChatResult{ToolCalls: []ai.ToolCall{{ID: "submit", Type: "function", Function: ai.ToolCallFunction{Name: "submit", Arguments: string(args)}}}}, nil
	}}
	parent := scope.New(nil)
	if err := parent.Provide(services.AgentRegistryKey, agent.NewRegistry(memberTool{}), nil); err != nil {
		fmt.Println("setup:", err)
		return
	}
	if err := parent.Provide(services.LLMKey, llm, nil); err != nil {
		fmt.Println("setup:", err)
		return
	}
	if err := parent.Provide(services.LoopConfigKey, services.LoopConfig{MaxTurns: 2}, nil); err != nil {
		fmt.Println("setup:", err)
		return
	}
	members := []Member{}
	for _, letter := range []string{"a", "b", "c"} {
		p := profile.Profile{
			Name:   "count-" + letter,
			System: "count letters",
			Result: &services.ResultSpec{Name: "submit", Schema: json.RawMessage(`{"type":"object","properties":{"value":{"type":"number"}},"required":["value"],"additionalProperties":false}`)},
		}
		members = append(members, Member{Profile: p, Input: services.RunRequest{Messages: []ai.Message{{Role: "user", Content: letter}}}})
	}
	res, err := Run(context.Background(), parent, members, Config{MaxParallel: 3, MemberTimeout: 5 * time.Second}, nil)
	if err != nil {
		fmt.Println("run:", err)
		return
	}
	totals := Rules[int]{{Name: "all-structured", Match: func(rs []MemberResult) bool {
		for _, r := range rs {
			if r.Err != nil || r.Outcome.Structured == nil {
				return false
			}
		}
		return true
	}, Output: func(rs []MemberResult) int {
		sum := 0
		for _, r := range rs {
			var v struct {
				Value int `json:"value"`
			}
			_ = json.Unmarshal(r.Outcome.Structured, &v)
			sum += v.Value
		}
		return sum
	}}}
	value, rule, ok := totals.Reduce(res.Members)
	if !ok {
		fmt.Println("no rule matched")
		return
	}
	fmt.Println(rule, strconv.Itoa(value))
	// Output: all-structured 9
}
