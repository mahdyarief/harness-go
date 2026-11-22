package profile

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
	"hsg/internal/lib/agent"
	"hsg/internal/lib/ai"
)

type testTool struct{ name string }

func (t testTool) Name() string        { return t.name }
func (t testTool) Description() string { return "test tool" }
func (t testTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
}
func (t testTool) ReadOnly() bool { return true }
func (t testTool) Execute(context.Context, json.RawMessage) (agent.ToolResult, error) {
	return agent.ToolResult{Output: `{"ok":true}`}, nil
}

func TestLoadProfileFromEmbeddedMarkdown(t *testing.T) {
	p, err := Load("default")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "default" || !strings.Contains(p.System, "generic assistant") {
		t.Fatalf("unexpected profile: %+v", p)
	}
}

func TestProfileHashStableAcrossToolOrdering(t *testing.T) {
	a := Profile{System: "prompt", Tools: []string{"b", "a"}}
	b := Profile{System: "prompt", Tools: []string{"a", "b"}}
	if Hash(a) != Hash(b) {
		t.Fatal("equivalent profiles have different hashes")
	}
	b.System = "different"
	if Hash(a) == Hash(b) {
		t.Fatal("profile hash ignored system prompt")
	}
}

func TestMountProfileBuildsRestrictedChild(t *testing.T) {
	root := scope.New(nil)
	registry := agent.NewRegistry(testTool{name: "read"}, testTool{name: "hidden"})
	if err := root.Provide(services.AgentRegistryKey, registry, nil); err != nil {
		t.Fatal(err)
	}
	if err := root.Provide(services.LoopConfigKey, services.LoopConfig{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := root.Provide(services.LLMKey, &testLLM{}, nil); err != nil {
		t.Fatal(err)
	}
	child, err := MountProfile(root, Profile{Name: "reader", System: "read only", Tools: []string{"read"}})
	if err != nil {
		t.Fatal(err)
	}
	tools, ok := scope.Require(child, services.ToolsKey)
	if !ok {
		t.Fatal("child tools missing")
	}
	if _, ok := tools.Lookup("read"); !ok {
		t.Fatal("allowed tool missing")
	}
	if _, ok := tools.Lookup("hidden"); ok {
		t.Fatal("non-allowlisted tool exposed")
	}
	if _, ok := scope.Require(child, services.LLMKey); !ok {
		t.Fatal("child did not inherit llm")
	}
	if cfg, ok := scope.Require(child, services.LoopConfigKey); !ok || cfg.MaxTurns == 0 {
		t.Fatalf("child loop config = %+v, present %v", cfg, ok)
	}
	if prompt, ok := scope.Require(child, services.SystemPromptKey); !ok || prompt != "read only" {
		t.Fatalf("system prompt = %q, present %v", prompt, ok)
	}
}

func TestTwoProfilesOnOneParentAreIsolated(t *testing.T) {
	root := scope.New(nil)
	if err := root.Provide(services.AgentRegistryKey, agent.NewRegistry(testTool{name: "read"}, testTool{name: "write"}), nil); err != nil {
		t.Fatal(err)
	}
	if err := root.Provide(services.LoopConfigKey, services.LoopConfig{}, nil); err != nil {
		t.Fatal(err)
	}
	reader, err := MountProfile(root, Profile{Name: "reader", Tools: []string{"read"}})
	if err != nil {
		t.Fatal(err)
	}
	writer, err := MountProfile(root, Profile{Name: "writer", Tools: []string{"write"}, LoopConfig: services.LoopConfig{MaxTurns: 5}})
	if err != nil {
		t.Fatal(err)
	}
	readerTools, _ := scope.Require(reader, services.ToolsKey)
	if _, ok := readerTools.Lookup("write"); ok {
		t.Fatal("reader sees writer's tool")
	}
	writerTools, _ := scope.Require(writer, services.ToolsKey)
	if _, ok := writerTools.Lookup("read"); ok {
		t.Fatal("writer sees reader's tool")
	}
	if cfg, _ := scope.Require(writer, services.LoopConfigKey); cfg.MaxTurns != 5 {
		t.Fatalf("writer MaxTurns = %d; want 5", cfg.MaxTurns)
	}
	if cfg, _ := scope.Require(reader, services.LoopConfigKey); cfg.MaxTurns == 5 {
		t.Fatal("reader inherited writer's budget")
	}
	if rootCfg, _ := scope.Require(root, services.LoopConfigKey); rootCfg.MaxTurns == 5 {
		t.Fatal("profile budget leaked into parent")
	}
}

func TestProfileHashChangesWithOnePromptCharacter(t *testing.T) {
	a := Profile{System: "prompt"}
	b := Profile{System: "prompt."}
	if Hash(a) == Hash(b) {
		t.Fatal("one-character prompt change did not change the hash")
	}
}

func TestMountProfileRejectsToolOutsideParentAllowlist(t *testing.T) {
	root := scope.New(nil)
	if err := root.Provide(services.AgentRegistryKey, agent.NewRegistry(testTool{name: "read"}), nil); err != nil {
		t.Fatal(err)
	}
	if err := root.Provide(services.ToolAllowlistKey, []string{"read"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := MountProfile(root, Profile{Name: "reader", Tools: []string{"read"}}); err != nil {
		t.Fatalf("in-allowlist tool rejected: %v", err)
	}
	wide := scope.New(nil)
	if err := wide.Provide(services.AgentRegistryKey, agent.NewRegistry(testTool{name: "read"}, testTool{name: "write"}), nil); err != nil {
		t.Fatal(err)
	}
	if err := wide.Provide(services.ToolAllowlistKey, []string{"read"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := MountProfile(wide, Profile{Name: "writer", Tools: []string{"write"}}); err == nil {
		t.Fatal("tool beyond parent allowlist accepted")
	}
}

func TestMountProfileRejectsUnknownTool(t *testing.T) {
	root := scope.New(nil)
	if err := root.Provide(services.AgentRegistryKey, agent.NewRegistry(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := MountProfile(root, Profile{Name: "reader", Tools: []string{"missing"}}); err == nil {
		t.Fatal("unknown tool accepted")
	}
}

type testLLM struct{}

func (*testLLM) MaxTokens() int { return 1024 }
func (*testLLM) ChatStream(context.Context, []ai.Message, ai.ChatOptions, func(ai.StreamDelta) error) (ai.ChatResult, error) {
	return ai.ChatResult{}, nil
}
