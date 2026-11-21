package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"hsg/internal/dtos"
	"hsg/internal/models"
	"hsg/internal/repositories"
)

// Proves the whole hybrid seam: scope authorization, config snapshot, credential
// decryption, pinned-model embedding call, hybrid SQL, and rank fusion.
func TestHybridSearcherEndToEnd(t *testing.T) {
	dsn := os.Getenv("EMBEDDING_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("EMBEDDING_TEST_DB_DSN is not set")
	}
	ctx := context.Background()
	root, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })

	// Random immutable revision keeps reruns idempotent: profiles are unique per
	// fingerprint, and the fake server pins the header to this revision.
	revision := strings.Repeat(strings.ReplaceAll(uuid.NewString(), "-", ""), 2)[:40]
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Embedding-Revision") != revision {
			t.Errorf("revision header = %q", r.Header.Get("X-Embedding-Revision"))
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		vec := make([]float64, models.KnowledgeEmbeddingDimensions)
		vec[0] = 1
		w.Header().Set("X-Embedding-Revision", revision)
		json.NewEncoder(w).Encode(map[string]any{
			"object": "list", "model": models.KnowledgeEmbeddingModel,
			"data": []any{map[string]any{"object": "embedding", "index": 0, "embedding": vec}},
		})
	}))
	defer server.Close()

	role := "hybrid_runtime_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	mustExec(t, root, `DO $$ BEGIN IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='`+role+`') THEN CREATE ROLE `+role+` NOLOGIN NOSUPERUSER NOBYPASSRLS; END IF; END $$`)
	mustExec(t, root, `GRANT USAGE ON SCHEMA public TO `+role)
	mustExec(t, root, `GRANT SELECT ON users,tenants,tenant_memberships,knowledge_documents,knowledge_chunks,knowledge_embedding_profiles,knowledge_embedding_config,knowledge_chunk_embeddings TO `+role)
	t.Cleanup(func() { root.Exec(`DROP OWNED BY ` + role + `; DROP ROLE IF EXISTS ` + role) })

	runtime, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	runtime.SetMaxOpenConns(1)
	t.Cleanup(func() { runtime.Close() })
	mustExec(t, runtime, `SET ROLE `+role)

	user := uuid.New()
	doc := uuid.New()
	chunk := uuid.New()
	profile := uuid.New()
	mustExec(t, root, `INSERT INTO users(id,name,email,role) VALUES($1,'Hybrid',$2,'super_user')`, user, user.String()+"@test.invalid")
	mustExec(t, root, `INSERT INTO knowledge_documents(id,title,filename,format,size_bytes,sha256,storage_key,chunk_count,uploaded_by) VALUES($1,'Panduan Hybrid','panduan.pdf','pdf',1234,$2,$3,1,$4)`, doc, strings.Repeat(strings.ReplaceAll(uuid.NewString(), "-", ""), 2)[:64], strings.Repeat(strings.ReplaceAll(uuid.NewString(), "-", ""), 2)[:64], user)
	mustExec(t, root, `INSERT INTO knowledge_chunks(id,document_id,position,heading_path,locator,content,title) VALUES($1,$2,0,'Bagian Satu','h1','verifikasi prosedur hybrid retrieval pgvector cosine similarity','Panduan Hybrid')`, chunk, doc)
	mustExec(t, root, `INSERT INTO knowledge_embedding_profiles(id,revision) VALUES($1,$2)`, profile, revision)
	t.Cleanup(func() {
		root.Exec(`DELETE FROM knowledge_chunk_embeddings WHERE chunk_id=$1`, chunk)
		root.Exec(`DELETE FROM knowledge_embedding_profiles WHERE id=$1`, profile)
		root.Exec(`DELETE FROM knowledge_chunks WHERE id=$1`, chunk)
		root.Exec(`DELETE FROM knowledge_documents WHERE id=$1`, doc)
		root.Exec(`DELETE FROM users WHERE id=$1`, user)
	})

	cfg := models.KnowledgeEmbeddingConfig{Endpoint: server.URL, Revision: revision, Model: models.KnowledgeEmbeddingModel, Dimensions: models.KnowledgeEmbeddingDimensions, Enabled: true}
	key := []byte(strings.Repeat("k", 32))
	sealed, err := embeddingCredential(key, cfg, []byte("test-secret"), true)
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, root, `UPDATE knowledge_embedding_config SET endpoint=$1,profile_id=$2,enabled=true,encrypted_credential=$3`, server.URL, profile, sealed)
	t.Cleanup(func() {
		root.Exec(`UPDATE knowledge_embedding_config SET enabled=false,profile_id=NULL,encrypted_credential=NULL`)
	})
	mustExec(t, root, `INSERT INTO knowledge_chunk_embeddings(chunk_id,profile_id,content_hash,embedding) SELECT $1,$2,encode(sha256((c.heading_path||E'\n'||c.content)::bytea),'hex'),(array_fill(1.0/sqrt(1024)::real,ARRAY[1024]))::vector FROM knowledge_chunks c WHERE c.id=$1`, chunk, profile)

	s := &hybridSearcher{
		repo:         repositories.NewKnowledgeSemanticRepository(runtime, root),
		key:          key,
		allowedHosts: []string{"127.0.0.1"},
		allowPrivate: true,
	}
	hits, err := s.Search(ctx, user, models.ChatScope{Kind: models.ChatPlatformPersonal}, "verifikasi prosedur hybrid retrieval pgvector cosine similarity", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("hybrid search returned no hits")
	}
	var hit *models.KnowledgeHit
	for i := range hits {
		if hits[i].ChunkID == chunk {
			hit = &hits[i]
			break
		}
	}
	if hit == nil {
		t.Fatalf("fixture chunk absent from %d fused hits", len(hits))
	}
	if hit.ContentHash == "" || hit.DocumentID != doc {
		t.Fatalf("hit metadata incomplete: %+v", *hit)
	}

	// No profile means the lexical fallback owns the answer, not an error.
	mustExec(t, root, `UPDATE knowledge_embedding_config SET enabled=false`)
	hits, err = s.Search(ctx, user, models.ChatScope{Kind: models.ChatPlatformPersonal}, "verifikasi", 5)
	if err != nil {
		t.Fatalf("disabled search: %v", err)
	}
	if hits != nil {
		t.Fatalf("disabled profile should fall back to lexical, got %d hits", len(hits))
	}
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("%v: %s", err, query)
	}
}

var _ = dtos.KnowledgeEmbeddingFingerprint
