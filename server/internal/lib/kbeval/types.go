// Package kbeval evaluates exported rankings offline; it never retrieves data.
package kbeval

import "io"

type Chunk struct {
	ID         string `json:"id"`
	DocumentID string `json:"document_id"`
	Text       string `json:"text"`
	Scope      string `json:"scope"`
}
type Label struct {
	ChunkID string  `json:"chunk_id"`
	Grade   float64 `json:"grade"`
}
type Query struct {
	ID       string  `json:"id"`
	Split    string  `json:"split"`
	Category string  `json:"category"`
	Text     string  `json:"text"`
	NoAnswer bool    `json:"no_answer"`
	Labels   []Label `json:"labels"`
}
type Fixture struct {
	Scope      string  `json:"scope"`
	ID         string  `json:"id"`
	Provenance string  `json:"provenance"`
	Chunks     []Chunk `json:"chunks"`
	Queries    []Query `json:"queries"`
}
type Candidate struct {
	ChunkID string `json:"chunk_id"`
	Rank    int    `json:"rank"`
}
type Ranking struct {
	QueryID    string      `json:"query_id"`
	Answered   *bool       `json:"answered"` // Explicit system decision, not inferred from RRF or candidate count.
	Candidates []Candidate `json:"candidates"`
}
type Rankings struct {
	FixtureID  string    `json:"fixture_id"`
	RunID      string    `json:"run_id"`
	Split      string    `json:"split"`
	Provenance string    `json:"provenance"`
	Results    []Ranking `json:"results"`
}

// Decode rejects unknown fields and trailing JSON rather than silently ignoring typos.
func Decode(r io.Reader, value any) error { return decode(r, value) }
