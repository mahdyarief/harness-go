package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"hsg/internal/lib/kbeval"
	"hsg/internal/models"
	"hsg/internal/repositories"
)

// Explicit opt-in only: both DSNs must name the disposable loopback kbeval DB.
func TestDisposableRepositoryBaseline(t *testing.T) {
	adminDSN, runtimeDSN := os.Getenv("TEST_KBEVAL_ADMIN_DSN"), os.Getenv("TEST_KBEVAL_RUNTIME_DSN")
	if adminDSN == "" || runtimeDSN == "" {
		t.Skip("requires disposable local kbeval database")
	}
	open := func(dsn string) *sql.DB {
		cfg, err := pgx.ParseConfig(dsn)
		if err != nil {
			t.Fatal("invalid test DSN")
		}
		ip := net.ParseIP(cfg.Host)
		if ip == nil || !ip.IsLoopback() || cfg.Database != "kbeval" || len(cfg.Fallbacks) != 0 {
			t.Fatal("refusing non-disposable database")
		}
		db := stdlib.OpenDB(*cfg)
		t.Cleanup(func() { db.Close() })
		return db
	}
	admin, runtime := open(adminDSN), open(runtimeDSN)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if unsafe, err := repositories.TenantRoleUnsafe(ctx, runtime); err != nil || unsafe {
		t.Fatalf("runtime role must pass repository isolation guard: unsafe=%v err=%v", unsafe, err)
	}
	file, err := os.Open(filepath.Join("..", "..", "internal", "lib", "kbeval", "testdata", "synthetic-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var fixture kbeval.Fixture
	if err := kbeval.Decode(file, &fixture); err != nil {
		t.Fatal(err)
	}
	if err := fixture.Validate(); err != nil {
		t.Fatal(err)
	}
	actor := uuid.New()
	if _, err := admin.ExecContext(ctx, `INSERT INTO users(id,name,email,role) VALUES($1,'synthetic eval','synthetic@example.invalid','super_user')`, actor); err != nil {
		t.Fatal(err)
	}
	docs, chunks := map[string]uuid.UUID{}, map[string]string{}
	t.Cleanup(func() {
		for _, id := range docs {
			admin.Exec(`DELETE FROM knowledge_chunks WHERE document_id=$1`, id)
			admin.Exec(`DELETE FROM knowledge_documents WHERE id=$1`, id)
		}
		admin.Exec(`DELETE FROM users WHERE id=$1`, actor)
	})
	for position, chunk := range fixture.Chunks {
		id, exists := docs[chunk.DocumentID]
		if !exists {
			id = uuid.New()
			docs[chunk.DocumentID] = id
			_, err = admin.ExecContext(ctx, `INSERT INTO knowledge_documents(id,title,filename,format,size_bytes,char_count,chunk_count,storage_key,sha256,uploaded_by) VALUES($1,'Synthetic fixture','synthetic.txt','txt',1,1,1,$2,$2,$3)`, id, fmt.Sprintf("%x", sha256.Sum256([]byte(id.String()))), actor)
			if err != nil {
				t.Fatal(err)
			}
		}
		_, err = admin.ExecContext(ctx, `INSERT INTO knowledge_chunks(document_id,position,locator,content) VALUES($1,$2,$3,$4)`, id, position, chunk.ID, chunk.Text)
		if err != nil {
			t.Fatal(err)
		}
		chunks[id.String()+"/"+chunk.ID] = chunk.ID
	}
	documentIDs := make([]string, 0, len(docs))
	for _, id := range docs {
		documentIDs = append(documentIDs, id.String())
	}
	repo := repositories.NewKnowledgeRepository(runtime)
	control, err := repo.Search(ctx, actor, models.ChatScope{Kind: models.ChatPlatformPersonal}, "filter", 5)
	if err != nil || len(control) != 5 {
		t.Fatalf("fixture visibility control: hits=%d err=%v", len(control), err)
	}
	control, err = repo.Search(ctx, actor, models.ChatScope{Kind: models.ChatPlatformPersonal}, "VX-001", 5)
	if err != nil || len(control) == 0 {
		t.Fatalf("fixture exact-code control: hits=%d err=%v", len(control), err)
	}
	t.Logf("fixture controls: filter=5 exact-code=%d; global scope is visible", len(control))
	// A verbatim fixture passage must be retrievable through the same runtime
	// role and ID mapping, otherwise zero metrics could be a harness failure.
	controlChunk := fixture.Chunks[0]
	controlHits, err := repo.Search(ctx, actor, models.ChatScope{Kind: models.ChatPlatformPersonal}, controlChunk.Text, 5)
	if err != nil {
		t.Fatal(err)
	}
	controlFound := false
	for _, hit := range controlHits {
		if chunks[hit.DocumentID.String()+"/"+hit.Locator] == controlChunk.ID {
			controlFound = true
		}
	}
	if !controlFound {
		t.Fatal("baseline harness failed to retrieve its verbatim fixture control")
	}
	t.Logf("verbatim fixture control retrieved: %s", controlChunk.ID)
	// Score the retrieved control through the same ID mapping and evaluator.
	// Do not change the natural-language fixture merely to raise its baseline.
	positive := kbeval.Fixture{ID: "sql-positive-control", Scope: "global", Provenance: "synthetic verbatim functional control, NOT semantic quality", Chunks: fixture.Chunks,
		Queries: []kbeval.Query{
			{ID: "control", Split: "dev", Category: "verbatim", Text: controlChunk.Text, Labels: []kbeval.Label{{ChunkID: controlChunk.ID, Grade: 3}}},
			{ID: "heldout-control-placeholder", Split: "heldout", Category: "no_answer", Text: "Unmeasured heldout control placeholder", NoAnswer: true},
		}}
	answered := len(controlHits) > 0
	controlRanking := kbeval.Ranking{QueryID: "control", Answered: &answered}
	for i, hit := range controlHits {
		controlRanking.Candidates = append(controlRanking.Candidates, kbeval.Candidate{ChunkID: chunks[hit.DocumentID.String()+"/"+hit.Locator], Rank: i + 1})
	}
	positiveReport, err := kbeval.Evaluate(positive, kbeval.Rankings{FixtureID: positive.ID, RunID: "sql-positive-control", Split: "dev", Provenance: positive.Provenance, Results: []kbeval.Ranking{controlRanking}})
	if err != nil {
		t.Fatal(err)
	}
	if positiveReport.Summary.Recall50 == nil || *positiveReport.Summary.Recall50 != 1 || positiveReport.Summary.NDCG5 == nil || *positiveReport.Summary.NDCG5 <= 0 {
		t.Fatalf("positive SQL control failed scoring: %+v", positiveReport.Summary)
	}
	t.Logf("functional scoring control: recall@50=%.3f ndcg@5=%.3f", *positiveReport.Summary.Recall50, *positiveReport.Summary.NDCG5)
	// Search is a final, deduplicated five-passage API, not a Recall@50
	// candidate source. Measure the full lexical shortlist independently.
	for _, branch := range []struct {
		name   string
		limit  int
		search func(context.Context, uuid.UUID, models.ChatScope, string, int, ...models.KnowledgeFilter) ([]models.KnowledgeHit, error)
	}{{"bounded5", 5, repo.Search}, {"candidates50", 50, repo.SearchCandidates}} {
		var latencies []time.Duration
		for _, split := range []string{"dev", "heldout"} {
			strictMatches, emptyRankings := 0, 0
			rankings := kbeval.Rankings{FixtureID: fixture.ID, RunID: "disposable-sql-" + branch.name + "-" + split, Split: split, Provenance: fmt.Sprintf("Actual local PostgreSQL lexical repository %s, limit=%d; synthetic generated qrels, NOT semantic quality; answered means nonempty retrieval, not LLM answer", branch.name, branch.limit)}
			for _, query := range fixture.Queries {
				if query.Split != split {
					continue
				}
				started := time.Now()
				hits, err := branch.search(ctx, actor, models.ChatScope{Kind: models.ChatPlatformPersonal}, query.Text, branch.limit)
				latencies = append(latencies, time.Since(started))
				if err != nil {
					t.Fatal(err)
				}
				if len(hits) == 0 {
					emptyRankings++
					var matches int
					err := admin.QueryRowContext(ctx, `SELECT count(*) FROM knowledge_chunks
					WHERE document_id=ANY($1::uuid[])
					AND search @@ websearch_to_tsquery('indonesian',$2)`, documentIDs, query.Text).Scan(&matches)
					if err != nil {
						t.Fatal(err)
					}
					strictMatches += matches
					if matches != 0 {
						t.Fatalf("empty repository result despite %d strict fixture matches for %s", matches, query.ID)
					}
				}
				answered := len(hits) > 0
				row := kbeval.Ranking{QueryID: query.ID, Answered: &answered}
				for i, hit := range hits {
					id := chunks[hit.DocumentID.String()+"/"+hit.Locator]
					if id == "" {
						t.Fatal("repository returned a non-fixture hit")
					}
					row.Candidates = append(row.Candidates, kbeval.Candidate{ChunkID: id, Rank: i + 1})
				}
				rankings.Results = append(rankings.Results, row)
			}
			t.Logf("empty rankings branch=%s split=%s count=%d matching fixture rows under strict websearch AND=%d", branch.name, split, emptyRankings, strictMatches)
			report, err := kbeval.Evaluate(fixture, rankings)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(report.Summary)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("measured %s split=%s summary=%s", branch.name, split, raw)
			if dir := os.Getenv("TEST_KBEVAL_OUTPUT_DIR"); dir != "" {
				raw, err := json.MarshalIndent(rankings, "", "  ")
				if err != nil {
					t.Fatal(err)
				}
				name := "baseline-" + split + ".json"
				if branch.name != "bounded5" {
					name = "baseline-" + branch.name + "-" + split + ".json"
				}
				if err := os.WriteFile(filepath.Join(dir, name), append(raw, '\n'), 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		p95 := latencies[(95*len(latencies)+99)/100-1]
		t.Logf("lexical repository latency branch=%s samples=%d p95_ms=%.3f target_ms=300 (tiny synthetic fixture; includes guard/transaction; not isolated SQL or full pipeline)", branch.name, len(latencies), float64(p95)/float64(time.Millisecond))
	}
}
