package repositories

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"hsg/internal/models"
)

// Exercises the hybrid candidate SQL against a migrated disposable database.
// The runtime connection assumes a non-owner, NOBYPASSRLS role so the policies
// in knowledge_base and knowledge_embeddings actually apply.
func TestKnowledgeSemanticCandidatesDatabase(t *testing.T) {
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

	role := "semantic_runtime_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
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

	platformUser := uuid.New()
	docID := uuid.New()
	chunkID := uuid.New()
	profileID := uuid.New()
	// A random immutable revision keeps reruns idempotent: the profile table has
	// a unique fingerprint derived from it.
	revision := strings.Repeat(strings.ReplaceAll(uuid.NewString(), "-", ""), 2)[:40]
	mustExec(t, root, `INSERT INTO users(id,name,email,role) VALUES($1,'Semantic',$2,'super_user')`, platformUser, platformUser.String()+"@test.invalid")
	mustExec(t, root, `INSERT INTO knowledge_documents(id,title,filename,format,size_bytes,sha256,storage_key,chunk_count,uploaded_by) VALUES($1,'Panduan Verifikasi','panduan.pdf','pdf',1234,$2,$3,1,$4)`, docID, strings.Repeat(strings.ReplaceAll(uuid.NewString(), "-", ""), 2)[:64], strings.Repeat(strings.ReplaceAll(uuid.NewString(), "-", ""), 2)[:64], platformUser)
	mustExec(t, root, `INSERT INTO knowledge_chunks(id,document_id,position,heading_path,locator,content,title) VALUES($1,$2,0,'Bagian Satu','h1','verifikasi prosedur hybrid retrieval pgvector cosine similarity','Panduan Verifikasi')`, chunkID, docID)
	mustExec(t, root, `INSERT INTO knowledge_embedding_profiles(id,revision) VALUES($1,$2)`, profileID, revision)
	mustExec(t, root, `INSERT INTO knowledge_chunk_embeddings(chunk_id,profile_id,content_hash,embedding) SELECT $1,$2,encode(sha256((c.heading_path||E'\n'||c.content)::bytea),'hex'),(array_fill(1.0/sqrt(1024)::real,ARRAY[1024]))::vector FROM knowledge_chunks c WHERE c.id=$1`, chunkID, profileID)
	t.Cleanup(func() {
		root.Exec(`DELETE FROM knowledge_chunk_embeddings WHERE chunk_id=$1`, chunkID)
		root.Exec(`DELETE FROM knowledge_embedding_profiles WHERE id=$1`, profileID)
		root.Exec(`DELETE FROM knowledge_chunks WHERE id=$1`, chunkID)
		root.Exec(`DELETE FROM knowledge_documents WHERE id=$1`, docID)
		root.Exec(`DELETE FROM users WHERE id=$1`, platformUser)
	})

	vector := make([]float32, models.KnowledgeEmbeddingDimensions)
	for i := range vector {
		vector[i] = float32(1.0 / 32.0)
	}
	repo := NewKnowledgeSemanticRepository(runtime, runtime)

	platform := models.ChatScope{Kind: models.ChatPlatformPersonal}
	lexical, semantic, err := repo.Candidates(ctx, platformUser, platform, "verifikasi prosedur hybrid retrieval pgvector cosine similarity", profileID, vector)
	if err != nil {
		t.Fatalf("platform candidates: %v", err)
	}
	if len(lexical) == 0 {
		t.Fatal("platform lexical branch returned no fixture chunk")
	}
	if len(semantic) == 0 {
		t.Fatal("platform semantic branch returned no fixture chunk")
	}
	// The disposable database may hold other global fixtures from earlier runs,
	// and platform scope sees every global chunk, so assert presence rather than
	// rank position.
	lexicalHit, semanticHit := findHit(lexical, chunkID), findHit(semantic, chunkID)
	if lexicalHit == nil {
		t.Fatalf("lexical branch missing fixture chunk in %d hits", len(lexical))
	}
	if semanticHit == nil {
		t.Fatalf("semantic branch missing fixture chunk in %d hits", len(semantic))
	}
	if semanticHit.ContentHash == "" {
		t.Fatal("semantic hit has no content hash")
	}

	// A non-super user with a valid tenant membership must not see the global
	// fixture row: knowledge_can_read requires row_tenant to match the pinned
	// tenant. Super user is rejected from tenant scope by design (platform only).
	member := uuid.New()
	mustExec(t, root, `INSERT INTO users(id,name,email,role) VALUES($1,'Member',$2,'user')`, member, member.String()+"@test.invalid")
	tenant := uuid.New()
	mustExec(t, root, `INSERT INTO tenants(id,name) VALUES($1,'Semantic Scope')`, tenant)
	mustExec(t, root, `INSERT INTO tenant_memberships(tenant_id,user_id,role) VALUES($1,$2,'reviewer')`, tenant, member)
	t.Cleanup(func() {
		root.Exec(`DELETE FROM tenant_memberships WHERE tenant_id=$1`, tenant)
		root.Exec(`DELETE FROM tenants WHERE id=$1`, tenant)
		root.Exec(`DELETE FROM users WHERE id=$1`, member)
	})
	scoped := models.ChatScope{Kind: models.ChatTenantPersonal, TenantID: tenant}
	lexical, semantic, err = repo.Candidates(ctx, member, scoped, "verifikasi prosedur hybrid retrieval pgvector cosine similarity", profileID, vector)
	if err != nil {
		t.Fatalf("tenant candidates: %v", err)
	}
	if len(lexical) != 0 || len(semantic) != 0 {
		t.Fatalf("tenant scope leaked rows: lexical=%d semantic=%d", len(lexical), len(semantic))
	}
}

func findHit(hits []models.KnowledgeHit, id uuid.UUID) *models.KnowledgeHit {
	for i := range hits {
		if hits[i].ChunkID == id {
			return &hits[i]
		}
	}
	return nil
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("%v: %s", err, query)
	}
}
