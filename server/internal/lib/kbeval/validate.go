package kbeval

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/google/uuid"
)

func decode(r io.Reader, value any) error {
	d := json.NewDecoder(r)
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return fmt.Errorf("expected one JSON value, got trailing data: %v", err)
	}
	return nil
}

func validID(s string) bool    { return s != "" && strings.TrimSpace(s) == s }
func validSplit(s string) bool { return s == "dev" || s == "heldout" }
func validScope(s string) bool {
	if s == "global" {
		return true
	}
	if !strings.HasPrefix(s, "tenant:") {
		return false
	}
	id, err := uuid.Parse(strings.TrimPrefix(s, "tenant:"))
	return err == nil && id != uuid.Nil && s == "tenant:"+id.String()
}

// Validate requires globally unique IDs, consistent labels and explicit splits.
// Identical normalized query text cannot cross the development/heldout boundary.
func (f Fixture) Validate() error {
	if !validID(f.ID) || strings.TrimSpace(f.Provenance) == "" {
		return fmt.Errorf("fixture id and provenance required")
	}
	if !validScope(f.Scope) {
		return fmt.Errorf("fixture requires global or tenant:UUID scope")
	}
	if len(f.Chunks) == 0 || len(f.Queries) == 0 {
		return fmt.Errorf("fixture chunks and queries required")
	}
	chunks := map[string]bool{}
	for _, c := range f.Chunks {
		if !validScope(c.Scope) || (c.Scope != "global" && c.Scope != f.Scope) {
			return fmt.Errorf("chunk %q outside fixture scope", c.ID)
		}
		if !validID(c.ID) || !validID(c.DocumentID) || strings.TrimSpace(c.Text) == "" || chunks[c.ID] {
			return fmt.Errorf("invalid or duplicate chunk %q", c.ID)
		}
		chunks[c.ID] = true
	}
	ids, texts, splits := map[string]bool{}, map[string]string{}, map[string]bool{}
	for _, q := range f.Queries {
		if !validID(q.ID) || ids[q.ID] || !validSplit(q.Split) || strings.TrimSpace(q.Text) == "" || strings.TrimSpace(q.Category) == "" {
			return fmt.Errorf("invalid or duplicate query %q", q.ID)
		}
		ids[q.ID], splits[q.Split] = true, true
		text := strings.ToLower(strings.Join(strings.Fields(q.Text), " "))
		if split, ok := texts[text]; ok && split != q.Split {
			return fmt.Errorf("query %q leaks across splits", q.ID)
		}
		texts[text] = q.Split
		seen, positives := map[string]bool{}, 0
		for _, l := range q.Labels {
			if !chunks[l.ChunkID] || seen[l.ChunkID] || math.IsNaN(l.Grade) || math.IsInf(l.Grade, 0) || l.Grade < 0 || l.Grade > 3 {
				return fmt.Errorf("query %q: invalid/duplicate label or grade outside [0,3]", q.ID)
			}
			seen[l.ChunkID] = true
			if l.Grade > 0 {
				positives++
			}
		}
		if (q.NoAnswer && positives > 0) || (!q.NoAnswer && positives == 0) {
			return fmt.Errorf("query %q: no_answer contradicts labels", q.ID)
		}
	}
	if !splits["dev"] || !splits["heldout"] {
		return fmt.Errorf("fixture requires both dev and heldout splits")
	}
	return nil
}

func validateRankings(f Fixture, r Rankings) error {
	if r.FixtureID != f.ID || !validID(r.RunID) || !validSplit(r.Split) || strings.TrimSpace(r.Provenance) == "" {
		return fmt.Errorf("rankings require matching fixture_id, run_id, split and provenance")
	}
	queries, chunks := map[string]Query{}, map[string]bool{}
	expected := 0
	for _, q := range f.Queries {
		queries[q.ID] = q
		if q.Split == r.Split {
			expected++
		}
	}
	for _, c := range f.Chunks {
		chunks[c.ID] = true
	}
	seen := map[string]bool{}
	for _, result := range r.Results {
		q, ok := queries[result.QueryID]
		if !ok || seen[result.QueryID] || q.Split != r.Split {
			return fmt.Errorf("unknown, duplicate or wrong-split query %q", result.QueryID)
		}
		seen[q.ID] = true
		if result.Answered == nil {
			return fmt.Errorf("query %q: explicit answered decision required", q.ID)
		}
		ids, ranks := map[string]bool{}, map[int]bool{}
		for _, c := range result.Candidates {
			if !chunks[c.ChunkID] || ids[c.ChunkID] || ranks[c.Rank] || c.Rank < 1 || c.Rank > len(result.Candidates) {
				return fmt.Errorf("query %q: unknown/duplicate chunk or invalid rank", q.ID)
			}
			ids[c.ChunkID], ranks[c.Rank] = true, true
		}
	}
	if len(seen) != expected {
		return fmt.Errorf("split %q: expected %d query results, got %d (missing queries cannot be omitted)", r.Split, expected, len(seen))
	}
	return nil
}
