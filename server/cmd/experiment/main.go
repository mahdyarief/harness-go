package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"hsg/internal/harness/experiment"
	"hsg/internal/harness/rules"
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
	subject := flags.String("subject", "", "subject JSON schema for the rules arm")
	ruleset := flags.String("rules", "", "rules YAML for the rules arm")
	group := flags.String("group", "", "rule group to evaluate")
	labelPath := flags.String("label-path", "label", "dot path to the label in label and output JSON")
	parallel := flags.Int("parallel", 1, "max concurrent arm x item runs")
	format := flags.String("format", "markdown", "report format: json, markdown, or csv")
	asOf := flags.String("as-of", "", "evaluate date-sensitive rules as of YYYY-MM-DD (default: today)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *dataset == "" || flags.NArg() != 0 {
		return fmt.Errorf("usage: experiment -dataset data.jsonl [-subject subject.json -rules rules.yaml -group g] [-as-of YYYY-MM-DD] [-format json|markdown|csv]")
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
	arms, err := buildArms(*subject, *ruleset, *group, clock)
	if err != nil {
		return err
	}
	if len(arms) == 0 {
		return fmt.Errorf("tidak ada arm: berikan -subject, -rules, dan -group")
	}
	res, err := experiment.Execute(context.Background(), ds, arms, experiment.Config{Parallel: *parallel})
	if err != nil {
		return err
	}
	report := experiment.BuildReport(res, ds, experiment.MetricsConfig{LabelPath: *labelPath})
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

func buildArms(subject, ruleset, group string, clock func() time.Time) ([]experiment.Arm, error) {
	if subject == "" || ruleset == "" || group == "" {
		return nil, nil
	}
	schema, err := os.ReadFile(subject)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(ruleset)
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
	return []experiment.Arm{experiment.RulesArm("rules", engine, group, clock)}, nil
}
