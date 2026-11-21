package kbeval

import (
	"math"
	"sort"
)

func score(q Query, r Ranking, docs map[string]string) Metrics {
	m := Metrics{Queries: 1, Answerable: 1}
	grades, relevantDocs, foundDocs := map[string]float64{}, map[string]bool{}, map[string]bool{}
	ideal := []float64{}
	for _, l := range q.Labels {
		if l.Grade > 0 {
			grades[l.ChunkID] = l.Grade
			ideal = append(ideal, l.Grade)
			relevantDocs[docs[l.ChunkID]] = true
		}
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(ideal)))
	hit20, hit50, dcg, idcg, rr := 0.0, 0.0, 0.0, 0.0, 0.0
	for i, grade := range ideal {
		if i < 5 {
			idcg += (math.Exp2(grade) - 1) / math.Log2(float64(i+2))
		}
	}
	candidates := append([]Candidate(nil), r.Candidates...)
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Rank < candidates[j].Rank })
	for _, c := range candidates {
		grade := grades[c.ChunkID]
		if grade <= 0 {
			continue
		}
		if c.Rank <= 20 {
			hit20++
		}
		if c.Rank <= 50 {
			hit50++
		}
		if c.Rank <= 5 {
			dcg += (math.Exp2(grade) - 1) / math.Log2(float64(c.Rank+1))
			if rr == 0 || 1/float64(c.Rank) > rr {
				rr = 1 / float64(c.Rank)
			}
			foundDocs[docs[c.ChunkID]] = true
		}
	}
	m.Recall20, m.Recall50 = number(hit20/float64(len(grades))), number(hit50/float64(len(grades)))
	m.NDCG5, m.MRR5 = number(dcg/idcg), number(rr)
	if len(relevantDocs) > 1 {
		m.MultiSource = 1
		m.SourceCoverage5 = number(float64(len(foundDocs)) / float64(len(relevantDocs)))
	}
	return m
}

func aggregate(rows []QueryResult) Metrics {
	var m Metrics
	sums, counts := [6]float64{}, [6]int{}
	for _, row := range rows {
		m.Queries++
		m.Answerable += row.Answerable
		m.NoAnswer += row.NoAnswer
		m.MultiSource += row.MultiSource
		for i, v := range []*float64{row.Recall20, row.Recall50, row.NDCG5, row.MRR5, row.SourceCoverage5, row.NoAnswerFalsePositive} {
			if v != nil {
				sums[i] += *v
				counts[i]++
			}
		}
	}
	values := []**float64{&m.Recall20, &m.Recall50, &m.NDCG5, &m.MRR5, &m.SourceCoverage5, &m.NoAnswerFalsePositive}
	for i, v := range values {
		if counts[i] > 0 {
			*v = number(sums[i] / float64(counts[i]))
		}
	}
	return m
}
