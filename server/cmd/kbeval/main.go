package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"hsg/internal/lib/kbeval"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("kbeval", flag.ContinueOnError)
	flags.SetOutput(stderr)
	fixture := flags.String("fixture", "", "fixture JSON with dev and heldout labels")
	rankings := flags.String("rankings", "", "exported rankings JSON for one complete split")
	format := flags.String("format", "json", "report format: json or csv (stdout)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *fixture == "" || *rankings == "" || flags.NArg() != 0 {
		return fmt.Errorf("usage: kbeval -fixture fixture.json -rankings rankings.json [-format json|csv]")
	}
	if *format != "json" && *format != "csv" {
		return fmt.Errorf("unsupported format %q", *format)
	}
	var f kbeval.Fixture
	var r kbeval.Rankings
	if err := readJSON(*fixture, &f); err != nil {
		return err
	}
	if err := readJSON(*rankings, &r); err != nil {
		return err
	}
	report, err := kbeval.Evaluate(f, r)
	if err != nil {
		return err
	}
	if *format == "csv" {
		return kbeval.WriteCSV(stdout, report)
	}
	return kbeval.WriteJSON(stdout, report)
}

func readJSON(path string, v any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := kbeval.Decode(f, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}
