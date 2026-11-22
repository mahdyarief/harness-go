package evidence_test

import (
	"encoding/json"
	"errors"
	"testing"

	"hsg/internal/harness/evidence"
	"hsg/internal/harness/services"
)

func TestCheckRejectsInventedRef(t *testing.T) {
	registry := evidence.NewRegistry("DOCUMENT")
	seen := []evidence.Item{{Kind: "DOCUMENT", Ref: "chunk-1"}}
	structured := json.RawMessage(`{"answer":"x","evidence":[{"kind":"DOCUMENT","ref":"chunk-9"}]}`)

	err := evidence.Check(structured, "evidence", registry, seen)
	var ungrounded *evidence.Ungrounded
	if !errors.As(err, &ungrounded) {
		t.Fatalf("err = %v; want EVIDENCE_UNGROUNDED", err)
	}
	if ungrounded.Code() != "EVIDENCE_UNGROUNDED" {
		t.Fatalf("code = %q", ungrounded.Code())
	}
	if len(ungrounded.Missing) != 1 || ungrounded.Missing[0].Ref != "chunk-9" {
		t.Fatalf("missing = %+v", ungrounded.Missing)
	}
}

func TestCheckAcceptsGroundedRef(t *testing.T) {
	registry := evidence.NewRegistry("DOCUMENT", "RULE")
	seen := []evidence.Item{
		{Kind: "DOCUMENT", Ref: "chunk-1"},
		{Kind: "RULE", Ref: "rule-7@2"},
	}
	structured := json.RawMessage(`{"evidence":[{"kind":"DOCUMENT","ref":"chunk-1"},{"kind":"RULE","ref":"rule-7@2"}]}`)
	if err := evidence.Check(structured, "evidence", registry, seen); err != nil {
		t.Fatalf("grounded refs rejected: %v", err)
	}
}

func TestCheckRejectsUnregisteredKind(t *testing.T) {
	registry := evidence.NewRegistry("DOCUMENT")
	seen := []evidence.Item{{Kind: "DOCUMENT", Ref: "chunk-1"}}
	// The ref exists in the run, but the kind was never registered: citing it
	// is still a schema violation.
	structured := json.RawMessage(`{"evidence":[{"kind":"MYSTERY","ref":"chunk-1"}]}`)
	err := evidence.Check(structured, "evidence", registry, seen)
	var ungrounded *evidence.Ungrounded
	if !errors.As(err, &ungrounded) {
		t.Fatalf("err = %v; want EVIDENCE_UNGROUNDED", err)
	}
}

func TestCheckReadsDottedPathAndEmptyCitations(t *testing.T) {
	registry := evidence.NewRegistry("DOCUMENT")
	seen := []evidence.Item{{Kind: "DOCUMENT", Ref: "chunk-1"}}
	structured := json.RawMessage(`{"result":{"citations":[{"kind":"DOCUMENT","ref":"chunk-1"}]}}`)
	if err := evidence.Check(structured, "result.citations", registry, seen); err != nil {
		t.Fatalf("dotted path rejected: %v", err)
	}
	// No citations: nothing to ground, no error.
	if err := evidence.Check(json.RawMessage(`{"answer":"x"}`), "evidence", registry, nil); err != nil {
		t.Fatalf("citation-free submission rejected: %v", err)
	}
	// Missing path: no citations found, no error.
	if err := evidence.Check(structured, "missing.path", registry, nil); err != nil {
		t.Fatalf("missing path rejected: %v", err)
	}
}

func TestResolverRunsForRegisteredKind(t *testing.T) {
	resolverErr := errors.New("rule retired")
	registry := evidence.NewRegistry()
	registry.Register("RULE", func(ref string) error {
		if ref == "rule-1@1" {
			return resolverErr
		}
		return nil
	})
	if err := registry.Resolve("RULE", "rule-1@1"); !errors.Is(err, resolverErr) {
		t.Fatalf("resolve err = %v", err)
	}
	if err := registry.Resolve("RULE", "rule-2@1"); err != nil {
		t.Fatalf("resolve err = %v", err)
	}
	if err := registry.Resolve("DOCUMENT", "x"); err == nil {
		t.Fatal("unregistered kind must not resolve")
	}
}

func TestDocumentItemKeepsAllReferenceFields(t *testing.T) {
	ref := services.Reference{
		DocumentID:  "doc-1",
		ChunkID:     "chunk-1",
		Title:       "Peraturan",
		Filename:    "pmk.pdf",
		Format:      "pdf",
		Locator:     "hal 4",
		HeadingPath: "Bab II > Pasal 3",
		Position:    4,
		Excerpt:     "Teks kutipan",
		Source:      "kb",
	}
	item := services.DocumentItem(ref)
	if item.Kind != "DOCUMENT" || item.Ref != "chunk-1" || item.Excerpt != ref.Excerpt {
		t.Fatalf("item = %+v", item)
	}
	want := map[string]string{
		"document_id":  "doc-1",
		"title":        "Peraturan",
		"filename":     "pmk.pdf",
		"format":       "pdf",
		"locator":      "hal 4",
		"heading_path": "Bab II > Pasal 3",
		"position":     "4",
		"source":       "kb",
	}
	for key, value := range want {
		if item.Meta[key] != value {
			t.Fatalf("meta[%q] = %q; want %q", key, item.Meta[key], value)
		}
	}
}

func TestDocumentItemFallsBackToDocumentID(t *testing.T) {
	item := services.DocumentItem(services.Reference{DocumentID: "doc-1", Excerpt: "x"})
	if item.Ref != "doc-1" {
		t.Fatalf("ref = %q; want document id", item.Ref)
	}
}

func TestExtractFindsNestedCitations(t *testing.T) {
	structured := json.RawMessage(`{
		"answer":"x",
		"claims":[
			{"text":"a","evidence":{"kind":"DOCUMENT","ref":"c-1"}},
			{"text":"b","evidence":[{"kind":"RULE","ref":"r-1@2"},{"kind":"GRAPH","ref":"e-9"}]}
		]
	}`)
	got := evidence.Extract(structured)
	if len(got) != 3 {
		t.Fatalf("citations = %+v", got)
	}
	if got[0].Kind != "DOCUMENT" || got[0].Ref != "c-1" {
		t.Fatalf("first = %+v", got[0])
	}
	if got[1].Kind != "GRAPH" || got[2].Kind != "RULE" {
		t.Fatalf("order = %+v", got)
	}
}
