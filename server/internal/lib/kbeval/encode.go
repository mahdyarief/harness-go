package kbeval

import (
	"encoding/csv"
	"encoding/json"
	"io"
	"strconv"
)

func WriteJSON(w io.Writer, r Report) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(r)
}

// WriteCSV includes summary, category and query rows in stable order. Empty
// numeric cells correspond to JSON null, not zero. Provenance is repeated so
// a standalone CSV does not lose the synthetic-fixture warning.
func WriteCSV(w io.Writer, r Report) error {
	c := csv.NewWriter(w)
	header := []string{"fixture_id", "fixture_provenance", "run_id", "run_provenance", "split", "row_type", "id", "category", "answered", "queries", "answerable", "no_answer", "multi_source", "recall_at_20", "recall_at_50", "ndcg_at_5", "mrr_at_5", "source_coverage_at_5", "no_answer_false_positive_rate", "scope"}
	if err := c.Write(header); err != nil {
		return err
	}
	write := func(kind, id, category, answered string, m Metrics) error {
		row := []string{r.FixtureID, r.FixtureProvenance, r.RunID, r.RunProvenance, r.Split, kind, id, category, answered, strconv.Itoa(m.Queries), strconv.Itoa(m.Answerable), strconv.Itoa(m.NoAnswer), strconv.Itoa(m.MultiSource)}
		for _, v := range []*float64{m.Recall20, m.Recall50, m.NDCG5, m.MRR5, m.SourceCoverage5, m.NoAnswerFalsePositive} {
			s := ""
			if v != nil {
				s = strconv.FormatFloat(*v, 'g', 17, 64)
			}
			row = append(row, s)
		}
		row = append(row, r.Scope)
		return c.Write(row)
	}
	if err := write("summary", "", "", "", r.Summary); err != nil {
		return err
	}
	for _, s := range r.Categories {
		if err := write("category", "", s.Category, "", s.Metrics); err != nil {
			return err
		}
	}
	for _, q := range r.Queries {
		if err := write("query", q.QueryID, q.Category, strconv.FormatBool(q.Answered), q.Metrics); err != nil {
			return err
		}
	}
	c.Flush()
	return c.Error()
}
