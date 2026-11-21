package kbeval

import "sort"

// Evaluate requires exactly one complete split. Rank positions are contiguous and
// unique; duplicate chunks are errors, never additional relevance hits.
func Evaluate(f Fixture, rankings Rankings) (Report, error) {
	if err := f.Validate(); err != nil {
		return Report{}, err
	}
	if err := validateRankings(f, rankings); err != nil {
		return Report{}, err
	}
	report := Report{Scope: f.Scope, SchemaVersion: 1, FixtureID: f.ID, FixtureProvenance: f.Provenance,
		RunID: rankings.RunID, RunProvenance: rankings.Provenance, Split: rankings.Split,
		Limitations: []string{"Offline exported rankings only; this evaluator does not execute SQL or load benchmarks.", "Labels and provenance supplied by fixture; scores do not establish human-ground-truth quality.", "Citation correctness, latency, ANN oracle recall, tenant isolation and resource usage are not measured."}}
	docs, queries := map[string]string{}, map[string]Query{}
	for _, c := range f.Chunks {
		docs[c.ID] = c.DocumentID
	}
	for _, q := range f.Queries {
		queries[q.ID] = q
	}
	results := append([]Ranking(nil), rankings.Results...)
	sort.Slice(results, func(i, j int) bool { return results[i].QueryID < results[j].QueryID })
	groups := map[string][]QueryResult{}
	for _, r := range results {
		q := queries[r.QueryID]
		row := QueryResult{QueryID: q.ID, Category: q.Category, Answered: *r.Answered, Metrics: Metrics{Queries: 1}}
		if q.NoAnswer {
			row.NoAnswer = 1
			row.NoAnswerFalsePositive = number(0)
			if row.Answered {
				row.NoAnswerFalsePositive = number(1)
			}
		} else {
			row.Metrics = score(q, r, docs)
		}
		report.Queries = append(report.Queries, row)
		groups[q.Category] = append(groups[q.Category], row)
	}
	report.Summary = aggregate(report.Queries)
	categories := make([]string, 0, len(groups))
	for c := range groups {
		categories = append(categories, c)
	}
	sort.Strings(categories)
	for _, c := range categories {
		report.Categories = append(report.Categories, Slice{Category: c, Metrics: aggregate(groups[c])})
	}
	return report, nil
}
