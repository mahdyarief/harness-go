package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"hsg/internal/lib/kbeval"
)

func fixtureArgs(format string) []string {
	root := filepath.Join("..", "..", "internal", "lib", "kbeval", "testdata")
	return []string{"-fixture", filepath.Join(root, "synthetic-v1.json"), "-rankings", filepath.Join(root, "illustrative-heldout.json"), "-format", format}
}
func TestCLIEndToEnd(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "kbeval")
	if output, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, output)
	}
	for _, format := range []string{"json", "csv"} {
		var output bytes.Buffer
		if err := run(fixtureArgs(format), &output, io.Discard); err != nil {
			t.Fatal(err)
		}
		if output.Len() == 0 {
			t.Fatal("empty in-process report")
		}
		t.Run(format, func(t *testing.T) {
			execute := func() []byte {
				cmd := exec.Command(binary, fixtureArgs(format)...)
				output, err := cmd.Output()
				if err != nil {
					t.Fatal(err)
				}
				return output
			}
			a, b := execute(), execute()
			if !bytes.Equal(a, b) {
				t.Fatal("CLI output is not deterministic")
			}
			if format == "json" {
				var r kbeval.Report
				if err := json.Unmarshal(a, &r); err != nil {
					t.Fatal(err)
				}
				if r.Split != "heldout" || r.Summary.Queries != 45 || r.Summary.NoAnswer != 9 || len(r.Queries) != 45 || len(r.Categories) != 7 {
					t.Fatalf("unexpected report %+v", r.Summary)
				}
				if !strings.Contains(r.FixtureProvenance, "NOT human ground truth") || len(r.Limitations) == 0 {
					t.Fatal("provenance/limitations missing")
				}
			} else {
				rows, err := csv.NewReader(bytes.NewReader(a)).ReadAll()
				if err != nil {
					t.Fatal(err)
				}
				if len(rows) != 54 || rows[1][5] != "summary" || rows[1][9] != "45" || rows[1][11] != "9" {
					t.Fatalf("unexpected CSV %d rows", len(rows))
				}
			}
		})
	}
	cmd := exec.Command(binary, "-fixture", "missing.json", "-rankings", "missing.json")
	if err := cmd.Run(); err == nil {
		t.Fatal("expected nonzero exit on invalid input")
	}
}
func TestCLIRejectsArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"-bad"}, {"-fixture", "missing"}, {"-fixture", "x", "-rankings", "y", "-format", "bad"}} {
		if err := run(args, io.Discard, io.Discard); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
