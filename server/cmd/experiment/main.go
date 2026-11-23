package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"hsg/internal/harness/agentloop"
	"hsg/internal/harness/cost"
	"hsg/internal/harness/experiment"
	"hsg/internal/harness/llm"
	"hsg/internal/harness/profile"
	"hsg/internal/harness/rules"
	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
	"hsg/internal/harness/team"
	"hsg/internal/lib/agent"
	"hsg/internal/lib/ai"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("experiment", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dataset := flags.String("dataset", "", "dataset JSONL, one {id,input,label} per line")
	armName := flags.String("arm", "rules", "arm to run: rules, profile, or team")
	subject := flags.String("subject", "", "subject JSON schema for the rules arm")
	ruleset := flags.String("rules", "", "rules YAML for the rules arm")
	group := flags.String("group", "", "rule group to evaluate")
	profileName := flags.String("profile", "", "embedded profile name for the profile arm")
	teamSpec := flags.String("team", "", "team spec YAML for the team arm")
	model := flags.String("model", "", "model name used by the replay store key")
	maxTokens := flags.Int("max-tokens", 0, "token ceiling reported by the replay adapter")
	pricesPath := flags.String("prices", "", "price table JSON: {version, prices}")
	recordDir := flags.String("record", "", "record every LLM call into DIR")
	replayDir := flags.String("replay", "", "answer every LLM call from DIR")
	endpoint := flags.String("endpoint", "", "chat completions URL for -record (must be https)")
	apiKey := flags.String("api-key", "", "provider credential for -record")
	allowedHosts := flags.String("allowed-hosts", "", "comma-separated allowlist for -record")
	labelPath := flags.String("label-path", "label", "dot path to the label in label and output JSON")
	citationPath := flags.String("citation-path", "", "dot path to the cited refs inside the output")
	parallel := flags.Int("parallel", 1, "max concurrent arm x item runs")
	memberTimeout := flags.Duration("member-timeout", time.Minute, "per-member timeout for the team arm")
	format := flags.String("format", "markdown", "report format: json, markdown, or csv")
	asOf := flags.String("as-of", "", "evaluate date-sensitive rules as of YYYY-MM-DD (default: today)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *dataset == "" || flags.NArg() != 0 {
		return fmt.Errorf("usage: experiment -dataset data.jsonl -arm rules|profile|team [flags]")
	}
	if *recordDir != "" && *replayDir != "" {
		return fmt.Errorf("-record dan -replay tidak boleh bersama")
	}
	if *armName != "rules" && *replayDir == "" && *recordDir == "" {
		return fmt.Errorf("arm %q memerlukan -replay atau -record (LLM dibutuhkan)", *armName)
	}
	var provider services.LLM
	var err error
	if *armName != "rules" || *replayDir != "" || *recordDir != "" {
		provider, err = buildLLM(llmFlags{
			replayDir:    *replayDir,
			recordDir:    *recordDir,
			model:        *model,
			maxTokens:    *maxTokens,
			endpoint:     *endpoint,
			apiKey:       *apiKey,
			allowedHosts: *allowedHosts,
		})
		if err != nil {
			return err
		}
	}
	clock := time.Now
	if *asOf != "" {
		at, err := time.Parse("2006-01-02", *asOf)
		if err != nil {
			return fmt.Errorf("as-of harus tanggal YYYY-MM-DD")
		}
		clock = func() time.Time { return at }
	}
	ds, err := experiment.LoadDataset(*dataset)
	if err != nil {
		return err
	}
	var table *cost.Table
	if *pricesPath != "" {
		table, err = loadPrices(*pricesPath)
		if err != nil {
			return err
		}
	}
	arms, err := buildArms(armSpec{
		arm:           *armName,
		subject:       *subject,
		ruleset:       *ruleset,
		group:         *group,
		profile:       *profileName,
		teamSpec:      *teamSpec,
		model:         *model,
		maxTokens:     *maxTokens,
		prices:        table,
		provider:      provider,
		memberTimeout: *memberTimeout,
	}, clock)
	if err != nil {
		return err
	}
	if len(arms) == 0 {
		return fmt.Errorf("tidak ada arm yang bisa dibangun")
	}
	cfg := experiment.Config{Parallel: *parallel}
	// A replay run must be byte-identical, so the wall clock never fills in an
	// unreported latency.
	cfg.NoWallClock = *replayDir != ""
	res, err := experiment.Execute(context.Background(), ds, arms, cfg)
	if err != nil {
		return err
	}
	metrics := experiment.MetricsConfig{
		LabelPath:     *labelPath,
		CitationPath:  *citationPath,
		EvidenceKinds: []string{"DOCUMENT", "RULE"},
	}
	report := experiment.BuildReport(res, ds, metrics)
	switch *format {
	case "json":
		return experiment.WriteJSON(stdout, report)
	case "markdown":
		return experiment.WriteMarkdown(stdout, report)
	case "csv":
		return experiment.WriteRatingCSV(stdout, res, ds)
	default:
		return fmt.Errorf("format %q tidak didukung", *format)
	}
}

type armSpec struct {
	arm           string
	subject       string
	ruleset       string
	group         string
	profile       string
	teamSpec      string
	model         string
	maxTokens     int
	prices        *cost.Table
	provider      services.LLM
	memberTimeout time.Duration
}

func buildArms(spec armSpec, clock func() time.Time) ([]experiment.Arm, error) {
	switch spec.arm {
	case "rules":
		return buildRulesArm(spec, clock)
	case "profile":
		return buildProfileArm(spec)
	case "team":
		return buildTeamArm(spec)
	default:
		return nil, fmt.Errorf("arm %q tidak didukung (rules, profile, team)", spec.arm)
	}
}

func buildRulesArm(spec armSpec, clock func() time.Time) ([]experiment.Arm, error) {
	if spec.subject == "" || spec.ruleset == "" || spec.group == "" {
		return nil, fmt.Errorf("arm rules memerlukan -subject, -rules, dan -group")
	}
	schema, err := os.ReadFile(spec.subject)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(spec.ruleset)
	if err != nil {
		return nil, err
	}
	list, err := rules.LoadYAML(f)
	f.Close()
	if err != nil {
		return nil, err
	}
	engine, err := rules.Load(schema, list)
	if err != nil {
		return nil, err
	}
	return []experiment.Arm{experiment.RulesArm("rules", engine, spec.group, clock)}, nil
}

func buildProfileArm(spec armSpec) ([]experiment.Arm, error) {
	if spec.profile == "" {
		return nil, fmt.Errorf("arm profile memerlukan -profile")
	}
	p, err := profile.Load(spec.profile)
	if err != nil {
		return nil, err
	}
	parent, err := replayScope(spec)
	if err != nil {
		return nil, err
	}
	arm, err := experiment.ProfileArm(p.Name, experiment.ProfileArmConfig{
		Parent: parent, Profile: p, Model: spec.model, Cost: spec.prices,
	})
	if err != nil {
		return nil, err
	}
	return []experiment.Arm{arm}, nil
}

type teamFile struct {
	Members []struct {
		Name    string `yaml:"name"`
		Profile string `yaml:"profile"`
		Model   string `yaml:"model"`
	} `yaml:"members"`
}

func buildTeamArm(spec armSpec) ([]experiment.Arm, error) {
	if spec.teamSpec == "" {
		return nil, fmt.Errorf("arm team memerlukan -team")
	}
	data, err := os.ReadFile(spec.teamSpec)
	if err != nil {
		return nil, err
	}
	var file teamFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	if len(file.Members) == 0 {
		return nil, fmt.Errorf("team spec tidak punya members")
	}
	parent, err := replayScope(spec)
	if err != nil {
		return nil, err
	}
	members := make([]team.Member, 0, len(file.Members))
	for _, m := range file.Members {
		if m.Name == "" || m.Profile == "" {
			return nil, fmt.Errorf("member team wajib punya name dan profile")
		}
		p, err := profile.Load(m.Profile)
		if err != nil {
			return nil, err
		}
		model := m.Model
		if model == "" {
			model = spec.model
		}
		members = append(members, team.Member{
			Model:   model,
			Profile: p,
			Input:   services.RunRequest{SystemText: p.System},
		})
	}
	return []experiment.Arm{experiment.TeamArm("team", experiment.TeamArmConfig{
		Parent:  parent,
		Members: members,
		Config: team.Config{
			MaxParallel:   len(members),
			MemberTimeout: spec.memberTimeout,
			Cost:          spec.prices,
		},
		Reducer: consensusRules(),
	})}, nil
}

// consensusRules accepts only an exact agreement between successful members.
func consensusRules() team.Rules[json.RawMessage] {
	return team.Rules[json.RawMessage]{{
		Name: "consensus",
		Match: func(results []team.MemberResult) bool {
			if len(results) == 0 {
				return false
			}
			first := memberOutput(results[0])
			if len(first) == 0 {
				return false
			}
			for _, result := range results[1:] {
				if result.Err != nil || string(memberOutput(result)) != string(first) {
					return false
				}
			}
			return results[0].Err == nil
		},
		Output: func(results []team.MemberResult) json.RawMessage {
			return memberOutput(results[0])
		},
	}}
}

func memberOutput(result team.MemberResult) json.RawMessage {
	if len(result.Outcome.Structured) > 0 {
		return result.Outcome.Structured
	}
	if result.Outcome.Content == "" {
		return nil
	}
	encoded, err := json.Marshal(map[string]any{"content": result.Outcome.Content})
	if err != nil {
		return nil
	}
	return encoded
}

// replayScope builds the minimal scope a profile or team arm needs. It carries
// no tools: a profile that names one fails at mount with a clear error instead
// of silently running without it.
func replayScope(spec armSpec) (*scope.Scope, error) {
	if spec.provider == nil {
		return nil, fmt.Errorf("arm ini memerlukan LLM (gunakan -replay atau -record)")
	}
	cfg, err := agentloop.ResolveLoopConfig(services.LoopConfig{})
	if err != nil {
		return nil, err
	}
	s := scope.New(nil)
	for _, provide := range []func() error{
		func() error { return s.Provide(services.LLMKey, spec.provider, nil) },
		func() error { return s.Provide(services.LoopConfigKey, cfg, nil) },
		func() error { return s.Provide(services.AgentRegistryKey, agent.NewRegistry(), nil) },
		func() error { return s.Provide(services.SandboxKey, "read-only", nil) },
		func() error { return s.Provide(services.ApprovalKey, "never", nil) },
	} {
		if err := provide(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

type priceFile struct {
	Version string                `json:"version"`
	Prices  map[string]cost.Rates `json:"prices"`
}

func loadPrices(path string) (*cost.Table, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var file priceFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("price table: %w", err)
	}
	table, err := cost.NewTable(file.Version, file.Prices)
	if err != nil {
		return nil, fmt.Errorf("price table: %w", err)
	}
	return &table, nil
}

type llmFlags struct {
	replayDir    string
	recordDir    string
	model        string
	maxTokens    int
	endpoint     string
	apiKey       string
	allowedHosts string
}

// buildLLM resolves the single adapter every arm shares. Replay never falls
// through to a provider; record keeps the provider authoritative while it
// writes the fixture.
func buildLLM(flags llmFlags) (services.LLM, error) {
	switch {
	case flags.replayDir != "":
		store, err := llm.OpenStore(flags.replayDir)
		if err != nil {
			return nil, err
		}
		return llm.ReplayAs(store, flags.model, flags.maxTokens), nil
	case flags.recordDir != "":
		store, err := llm.OpenStore(flags.recordDir)
		if err != nil {
			return nil, err
		}
		hosts := splitHosts(flags.allowedHosts)
		if flags.endpoint == "" || len(hosts) == 0 {
			return nil, fmt.Errorf("-record memerlukan -endpoint dan -allowed-hosts")
		}
		gateway, err := ai.NewGateway(ai.Config{
			Endpoint:     flags.endpoint,
			AllowedHosts: hosts,
			Model:        flags.model,
			APIKey:       flags.apiKey,
			MaxTokens:    flags.maxTokens,
		})
		if err != nil {
			return nil, err
		}
		return llm.Record(llm.New(gateway), store), nil
	default:
		return nil, fmt.Errorf("LLM tidak tersedia: berikan -replay atau -record")
	}
}

func splitHosts(raw string) []string {
	hosts := make([]string, 0, 4)
	for _, part := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			hosts = append(hosts, trimmed)
		}
	}
	return hosts
}
