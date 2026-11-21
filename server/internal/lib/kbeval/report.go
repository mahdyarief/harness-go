package kbeval

// Metrics use macro means over answerable queries only; nil means not applicable.
// SourceCoverage5 averages document recall only for multi-document positive labels.
// NoAnswerFalsePositive is answered=true / no-answer queries, not a score threshold.
type Metrics struct {
	Queries               int      `json:"queries"`
	Answerable            int      `json:"answerable"`
	NoAnswer              int      `json:"no_answer"`
	MultiSource           int      `json:"multi_source"`
	Recall20              *float64 `json:"recall_at_20"`
	Recall50              *float64 `json:"recall_at_50"`
	NDCG5                 *float64 `json:"ndcg_at_5"`
	MRR5                  *float64 `json:"mrr_at_5"`
	SourceCoverage5       *float64 `json:"source_coverage_at_5"`
	NoAnswerFalsePositive *float64 `json:"no_answer_false_positive_rate"`
}
type QueryResult struct {
	QueryID  string `json:"query_id"`
	Category string `json:"category"`
	Answered bool   `json:"answered"`
	Metrics
}
type Slice struct {
	Category string `json:"category"`
	Metrics
}
type Report struct {
	SchemaVersion     int           `json:"schema_version"`
	Scope             string        `json:"scope"`
	FixtureID         string        `json:"fixture_id"`
	FixtureProvenance string        `json:"fixture_provenance"`
	RunID             string        `json:"run_id"`
	RunProvenance     string        `json:"run_provenance"`
	Split             string        `json:"split"`
	Summary           Metrics       `json:"summary"`
	Categories        []Slice       `json:"categories"`
	Queries           []QueryResult `json:"queries"`
	Limitations       []string      `json:"limitations"`
}

func number(n float64) *float64 { return &n }
