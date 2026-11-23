package experiment

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hsg/internal/harness/agentloop"
	"hsg/internal/harness/llm"
	"hsg/internal/harness/profile"
	"hsg/internal/harness/rules"
	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
	"hsg/internal/harness/team"
	"hsg/internal/lib/agent"
	"hsg/internal/lib/ai"
)

type fixedLLM struct{ calls int }

func (f *fixedLLM) MaxTokens() int { return 128 }

func (f *fixedLLM) Model() string { return "m" }

func (f *fixedLLM) ChatStream(_ context.Context, _ []ai.Message, _ ai.ChatOptions, _ func(ai.StreamDelta) error) (ai.ChatResult, error) {
	f.calls++
	return ai.ChatResult{
		Content:          "",
		PromptTokens:     11,
		CompletionTokens: 3,
		ToolCalls: []ai.ToolCall{{
			ID:       "c1",
			Type:     "function",
			Function: ai.ToolCallFunction{Name: "submit", Arguments: `{"value":"ok"}`},
		}},
	}, nil
}

func armScope(t *testing.T, client services.LLM) *scope.Scope {
	t.Helper()
	cfg, err := agentloop.ResolveLoopConfig(services.LoopConfig{MaxTurns: 2})
	if err != nil {
		t.Fatal(err)
	}
	s := scope.New(nil)
	for _, provide := range []func() error{
		func() error { return s.Provide(services.AgentRegistryKey, agent.NewRegistry(), nil) },
		func() error { return s.Provide(services.LLMKey, client, nil) },
		func() error { return s.Provide(services.LoopConfigKey, cfg, nil) },
		func() error { return s.Provide(services.SandboxKey, "read-only", nil) },
		func() error { return s.Provide(services.ApprovalKey, "never", nil) },
	} {
		if err := provide(); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func submitProfile(name string) profile.Profile {
	return profile.Profile{
		Name:   name,
		System: "system " + name,
		Result: &services.ResultSpec{
			Name:   "submit",
			Schema: json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}`),
		},
	}
}

func demoEngine(t *testing.T) *rules.Engine {
	t.Helper()
	list, err := rules.LoadYAML(strings.NewReader(`
- id: high
  version: 1
  group: demo
  severity: warn
  when: score > 5
  message: skor tinggi
`))
	if err != nil {
		t.Fatal(err)
	}
	engine, err := rules.Load(json.RawMessage(`{"type":"object","properties":{"score":{"type":"number"}}}`), list)
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

func storeKeys(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func decisionRules() team.Rules[json.RawMessage] {
	return team.Rules[json.RawMessage]{{
		Name:  "all-structured",
		Match: func(members []team.MemberResult) bool { return len(members) == 2 },
		Output: func(members []team.MemberResult) json.RawMessage {
			return json.RawMessage(`{"value":"ok"}`)
		},
	}}
}

// TestCheckpointCReplaysThreeArms builds the rules, profile and team arms over
// one dataset and proves that a replay run reproduces the recorded report
// byte for byte.
func TestCheckpointCReplaysThreeArms(t *testing.T) {
	ds := Dataset{Items: []Item{
		{ID: "a", Input: json.RawMessage(`{"score":7}`)},
		{ID: "b", Input: json.RawMessage(`{"score":2}`)},
	}}
	group := "demo"
	engine := demoEngine(t)
	rulesArm := RulesArm("rules", engine, group, nil)

	storeDir := t.TempDir()
	store, err := llm.OpenStore(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	recorded := llm.Record(&fixedLLM{}, store)

	build := func(client services.LLM) []Arm {
		parent := armScope(t, client)
		pArm, err := ProfileArm("profile", ProfileArmConfig{Parent: parent, Profile: submitProfile("solo"), Model: "m"})
		if err != nil {
			t.Fatal(err)
		}
		teamArm := TeamArm("team", TeamArmConfig{
			Parent:  parent,
			Members: []team.Member{{Profile: submitProfile("one"), Model: "m"}, {Profile: submitProfile("two"), Model: "m"}},
			Config:  team.Config{MaxParallel: 2, MemberTimeout: time.Second},
			Reducer: decisionRules(),
		})
		return []Arm{rulesArm, pArm, teamArm}
	}

	first, err := Execute(context.Background(), ds, build(recorded), Config{Parallel: 1, NoWallClock: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(storeKeys(t, storeDir)) == 0 {
		t.Fatal("record mode wrote no fixture")
	}
	replayed := llm.ReplayAs(store, "m", 128)
	second, err := Execute(context.Background(), ds, build(replayed), Config{Parallel: 1, NoWallClock: true})
	if err != nil {
		t.Fatal(err)
	}

	cfg := MetricsConfig{LabelPath: "value"}
	var firstBuf, secondBuf bytes.Buffer
	if err := WriteJSON(&firstBuf, BuildReport(first, ds, cfg)); err != nil {
		t.Fatal(err)
	}
	if err := WriteJSON(&secondBuf, BuildReport(second, ds, cfg)); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstBuf.Bytes(), secondBuf.Bytes()) {
		firstLines := strings.Split(firstBuf.String(), "\n")
		secondLines := strings.Split(secondBuf.String(), "\n")
		for i := 0; i < len(firstLines) && i < len(secondLines); i++ {
			if firstLines[i] != secondLines[i] {
				t.Fatalf("baris %d berbeda:\nrecord: %q\nreplay: %q", i+1, firstLines[i], secondLines[i])
			}
		}
		t.Fatalf("panjang report berbeda: %d vs %d", len(firstLines), len(secondLines))
	}
	for _, arm := range second.ArmNames {
		for _, row := range second.ForArm(arm) {
			if row.Err != "" {
				t.Fatalf("arm %s item %s: %s", arm, row.ItemID, row.Err)
			}
		}
	}
}
