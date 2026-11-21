package main

import (
	"context"
	"math"
	"sort"
	"sync"
	"time"
)

type benchmarkReport struct {
	FixtureSHA256  string  `json:"fixture_sha256"`
	Scope          string  `json:"scope"`
	Queries        int     `json:"queries"`
	Iterations     int     `json:"iterations"`
	Concurrency    int     `json:"concurrency"`
	RequestedLimit int     `json:"requested_limit"`
	EffectiveLimit int     `json:"effective_limit"`
	Attempts       int     `json:"attempts"`
	Successes      int     `json:"successes"`
	Errors         int     `json:"errors"`
	Hits           int     `json:"hits"`
	Cancelled      bool    `json:"cancelled"`
	ElapsedMS      float64 `json:"elapsed_ms"`
	P50MS          float64 `json:"p50_ms"`
	P95MS          float64 `json:"p95_ms"`
	P99MS          float64 `json:"p99_ms"`
	QPS            float64 `json:"successful_qps"`
	Mode           string  `json:"mode"`
}

type observation struct {
	elapsed time.Duration
	hits    int
	failed  bool
}

// Nearest-rank percentiles include failed requests; QPS counts successes only.
func percentile(sorted []time.Duration, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	return float64(sorted[int(math.Ceil(p*float64(len(sorted))))-1]) / float64(time.Millisecond)
}

func runBenchmark(ctx context.Context, queries []string, iterations, concurrency int, timeout time.Duration, search func(context.Context, string) (int, error)) benchmarkReport {
	start := time.Now()
	report := benchmarkReport{Mode: "repository_search_lexical_bounded5"}
	jobs := make(chan string)
	results := make(chan observation, concurrency)
	var workers sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for query := range jobs {
				if ctx.Err() != nil {
					return
				}
				request, cancel := context.WithTimeout(ctx, timeout)
				began := time.Now()
				hits, err := search(request, query)
				failed := err != nil || request.Err() != nil
				cancel()
				results <- observation{time.Since(began), hits, failed}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for i := 0; i < iterations; i++ {
			for _, query := range queries {
				select {
				case <-ctx.Done():
					return
				case jobs <- query:
				}
			}
		}
	}()
	go func() { workers.Wait(); close(results) }()
	latencies := []time.Duration{}
	for result := range results {
		report.Attempts++
		latencies = append(latencies, result.elapsed)
		if result.failed {
			report.Errors++
		} else {
			report.Successes++
			report.Hits += result.hits
		}
	}
	elapsed := time.Since(start)
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	report.ElapsedMS = float64(elapsed) / float64(time.Millisecond)
	report.P50MS, report.P95MS, report.P99MS = percentile(latencies, .5), percentile(latencies, .95), percentile(latencies, .99)
	if elapsed > 0 {
		report.QPS = float64(report.Successes) / elapsed.Seconds()
	}
	report.Cancelled = ctx.Err() != nil
	return report
}
