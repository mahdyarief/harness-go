package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPrices(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prices.json")
	body := `{"version":"2026-11","prices":{"m1":{"input_per_million":1,"output_per_million":2}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	table, err := loadPrices(path)
	if err != nil {
		t.Fatal(err)
	}
	if table == nil || table.Version != "2026-11" {
		t.Fatalf("table = %+v", table)
	}

	if err := os.WriteFile(path, []byte(`{"prices":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPrices(path); err == nil {
		t.Fatal("JSON rusak harus ditolak")
	}
}

func TestSplitHosts(t *testing.T) {
	got := splitHosts(" a.example , ,b.example")
	if len(got) != 2 || got[0] != "a.example" || got[1] != "b.example" {
		t.Fatalf("hosts = %v", got)
	}
}

func TestBuildLLMReplayNeedsNoProvider(t *testing.T) {
	dir := t.TempDir()
	inner, err := buildLLM(llmFlags{replayDir: dir, model: "m", maxTokens: 10})
	if err != nil {
		t.Fatal(err)
	}
	if inner == nil {
		t.Fatal("replay adapter nil")
	}
}

func TestBuildLLMRecordNeedsEndpoint(t *testing.T) {
	if _, err := buildLLM(llmFlags{recordDir: t.TempDir()}); err == nil {
		t.Fatal("record tanpa endpoint harus ditolak")
	}
}

func TestBuildArmsRulesNeedsInputs(t *testing.T) {
	if _, err := buildArms(armSpec{arm: "rules"}, nil); err == nil {
		t.Fatal("rules tanpa subject/rules/group harus ditolak")
	}
	if _, err := buildArms(armSpec{arm: "nope"}, nil); err == nil {
		t.Fatal("arm tak dikenal harus ditolak")
	}
}
