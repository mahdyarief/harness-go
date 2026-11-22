package profile

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"

	"hsg/internal/harness/agentloop"
	"hsg/internal/harness/schema"
	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
	"hsg/internal/harness/tools"
	"hsg/internal/lib/agent"
)

//go:embed prompts/*.md
var prompts embed.FS

type Profile struct {
	Name       string               `yaml:"name"`
	System     string               `yaml:"system"`
	Tools      []string             `yaml:"tools"`
	Result     *services.ResultSpec `yaml:"result"`
	Preset     string               `yaml:"preset"`
	LoopConfig services.LoopConfig  `yaml:"loop_config"`
}

func (p *Profile) UnmarshalYAML(node *yaml.Node) error {
	p.Name = ""
	var raw struct {
		System     string               `yaml:"system"`
		Tools      []string             `yaml:"tools"`
		Result     *services.ResultSpec `yaml:"result"`
		Preset     string               `yaml:"preset"`
		LoopConfig services.LoopConfig  `yaml:"loop_config"`
	}
	if err := node.Decode(&raw); err != nil {
		return err
	}
	p.System, p.Tools, p.Result, p.Preset, p.LoopConfig = raw.System, raw.Tools, raw.Result, raw.Preset, raw.LoopConfig
	return nil
}

func (p Profile) Hash() string { return Hash(p) }

func parseFrontMatter(text string, dst any) (string, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return "", fmt.Errorf("missing YAML front matter")
	}
	rest := strings.TrimPrefix(text, "---\n")
	closing := strings.Index(rest, "\n---\n")
	if closing < 0 {
		return "", fmt.Errorf("malformed YAML front matter")
	}
	if err := yaml.Unmarshal([]byte(rest[:closing]), dst); err != nil {
		return "", err
	}
	return rest[closing+5:], nil
}

func Load(name string) (Profile, error) {
	data, err := prompts.ReadFile("prompts/" + name + ".md")
	if err != nil {
		return Profile{}, fmt.Errorf("load profile %q: %w", name, err)
	}
	text := string(data)
	var p Profile
	body, err := parseFrontMatter(text, &p)
	if err != nil {
		return Profile{}, fmt.Errorf("profile %q: %w", name, err)
	}
	p.Name = name
	if p.System == "" {
		p.System = body
	}
	return p, nil
}

func Hash(p Profile) string {
	names := append([]string(nil), p.Tools...)
	sort.Strings(names)
	payload, err := json.Marshal(struct {
		System     string
		Tools      []string
		Result     *services.ResultSpec
		LoopConfig services.LoopConfig
	}{p.System, names, p.Result, p.LoopConfig})
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

type profileSubmitTool struct {
	name        string
	description string
	raw         json.RawMessage
}

func (t *profileSubmitTool) Name() string                { return t.name }
func (t *profileSubmitTool) Description() string         { return t.description }
func (t *profileSubmitTool) Parameters() json.RawMessage { return t.raw }
func (t *profileSubmitTool) ReadOnly() bool              { return true }
func (t *profileSubmitTool) Terminal() bool              { return true }
func (t *profileSubmitTool) Execute(_ context.Context, args json.RawMessage) (agent.ToolResult, error) {
	compiled, err := schema.Compile(t.raw)
	if err != nil {
		return agent.ToolResult{}, err
	}
	if violations := compiled.Validate(args); len(violations) > 0 {
		return agent.ToolResult{}, fmt.Errorf("invalid structured result: %v", violations)
	}
	return agent.ToolResult{Output: string(args)}, nil
}

func MountProfile(parent *scope.Scope, p Profile) (*scope.Scope, error) {
	registry, ok := scope.Require(parent, services.AgentRegistryKey)
	if !ok {
		return nil, fmt.Errorf("agent registry missing")
	}
	if parentAllowlist, ok := scope.Require(parent, services.ToolAllowlistKey); ok && len(parentAllowlist) > 0 {
		allowed := make(map[string]bool, len(parentAllowlist))
		for _, name := range parentAllowlist {
			allowed[name] = true
		}
		for _, name := range p.Tools {
			if !allowed[name] {
				return nil, fmt.Errorf("profile %q: tool %q exceeds the parent allowlist", p.Name, name)
			}
		}
	}
	for _, name := range p.Tools {
		tool, ok := registry.Lookup(name)
		if !ok {
			return nil, fmt.Errorf("profile %q: unknown tool %q", p.Name, name)
		}
		if _, err := schema.Compile(tool.Parameters()); err != nil {
			return nil, fmt.Errorf("profile %q tool %q: %w", p.Name, name, err)
		}
	}
	if p.Result != nil {
		if p.Result.Name == "" {
			return nil, fmt.Errorf("profile %q: result tool name is empty", p.Name)
		}
		if _, err := schema.Compile(p.Result.Schema); err != nil {
			return nil, fmt.Errorf("profile %q result schema: %w", p.Name, err)
		}
	}
	cfg := p.LoopConfig
	if reflect.DeepEqual(cfg, services.LoopConfig{}) {
		cfg, _ = scope.Require(parent, services.LoopConfigKey)
	}
	cfg, err := agentloop.ResolveLoopConfig(cfg)
	if err != nil {
		return nil, err
	}
	if p.Result != nil {
		cfg.RequireStructured = p.Result.Name
	}
	child := parent.Child()
	if err := child.Provide(services.SystemPromptKey, p.System, nil); err != nil {
		return nil, err
	}
	if err := child.Provide(services.ToolAllowlistKey, append([]string(nil), p.Tools...), nil); err != nil {
		return nil, err
	}
	if err := child.Provide(services.LoopConfigKey, cfg, nil); err != nil {
		return nil, err
	}
	selected := make([]agent.Tool, 0, len(p.Tools)+1)
	for _, name := range p.Tools {
		tool, _ := registry.Lookup(name)
		selected = append(selected, tool)
	}
	if p.Result != nil {
		selected = append(selected, &profileSubmitTool{name: p.Result.Name, description: "Submit structured result", raw: p.Result.Schema})
	}
	adapted := tools.Adapt(agent.NewRegistry(selected...))
	if err := tools.ValidateSchemas(adapted); err != nil {
		return nil, err
	}
	if err := child.Provide(services.ToolsKey, tools.WrapTools(child, adapted, cfg), nil); err != nil {
		return nil, err
	}
	return child, nil
}
