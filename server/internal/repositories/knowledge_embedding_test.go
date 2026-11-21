package repositories

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"hsg/internal/dtos"
	"hsg/internal/models"
)

// Uses an already migrated disposable database; never resets schemas or shared fixtures.
func TestKnowledgeEmbeddingDatabase(t *testing.T) {
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
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := root.ExecContext(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`DO $$ BEGIN IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='embedding_runtime_a2616d55') THEN CREATE ROLE embedding_runtime_a2616d55 NOLOGIN NOSUPERUSER NOBYPASSRLS; END IF; IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='embedding_worker_a2616d55') THEN CREATE ROLE embedding_worker_a2616d55 NOLOGIN NOSUPERUSER NOBYPASSRLS; END IF; END $$`)
	exec(`GRANT hsg_embedding_worker TO embedding_worker_a2616d55; GRANT USAGE ON SCHEMA public TO embedding_runtime_a2616d55,embedding_worker_a2616d55; GRANT SELECT ON users,tenants,tenant_memberships,knowledge_documents,knowledge_chunks,knowledge_embedding_profiles,knowledge_embedding_config,knowledge_embedding_jobs,knowledge_chunk_embeddings TO embedding_runtime_a2616d55; GRANT INSERT,UPDATE ON knowledge_embedding_profiles,knowledge_embedding_config,knowledge_embedding_jobs TO embedding_runtime_a2616d55`)
	roleDB := func(role string) *sql.DB {
		t.Helper()
		db, e := sql.Open("pgx", dsn)
		if e != nil {
			t.Fatal(e)
		}
		db.SetMaxOpenConns(1)
		if _, e = db.Exec("SET ROLE " + role); e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { db.Close() })
		return db
	}
	runtime := roleDB("embedding_runtime_a2616d55")
	workerDB := roleDB("embedding_worker_a2616d55")
	worker := NewKnowledgeEmbeddingWorkerRepository(workerDB)
	repo := NewKnowledgeEmbeddingRepository(runtime)
	if err = repo.ValidateRuntimeRole(ctx); err != nil {
		t.Fatal("runtime guard", err)
	}
	if err = worker.ValidateRole(ctx); err != nil {
		t.Fatal(err)
	}
	if err = NewKnowledgeEmbeddingWorkerRepository(root).ValidateRole(ctx); err == nil {
		t.Fatal("owner worker accepted")
	}
	admin, user, tenant, doc := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	exec(`INSERT INTO users(id,name,email,role) VALUES($1,'Admin',$2,'super_user'),($3,'Member',$4,'user')`, admin, admin.String()+"@test.invalid", user, user.String()+"@test.invalid")
	exec(`INSERT INTO tenants(id,name) VALUES($1,'Synthetic')`, tenant)
	exec(`INSERT INTO tenant_memberships(tenant_id,user_id,role) VALUES($1,$2,'tenant_admin')`, tenant, user)
	t.Cleanup(func() {
		root.Exec(`DELETE FROM knowledge_documents WHERE id=$1`, doc)
		root.Exec(`DELETE FROM tenant_memberships WHERE tenant_id=$1`, tenant)
		root.Exec(`DELETE FROM tenants WHERE id=$1`, tenant)
		root.Exec(`DELETE FROM users WHERE id IN ($1,$2)`, admin, user)
	})
	cfg, err := repo.Get(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Endpoint = "https://embed.test.invalid/v1/embeddings"
	cfg.Model = models.KnowledgeEmbeddingModel
	cfg.Revision = strings.Repeat("a", 40)
	cfg.Dimensions = 1024
	cfg.Enabled = true
	if err = repo.Save(ctx, admin, cfg, cfg.Generation); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO knowledge_documents(id,tenant_id,title,filename,format,size_bytes,sha256,storage_key,char_count,chunk_count) VALUES($1,$2,'Synthetic','doc.txt','txt',10,$3,$4,10,1)`, doc, tenant, strings.ReplaceAll(doc.String(), "-", "")+strings.ReplaceAll(doc.String(), "-", ""), strings.Repeat("c", 64))
	exec(`INSERT INTO knowledge_chunks(document_id,tenant_id,position,content) VALUES($1,$2,0,'Synthetic evidence')`, doc, tenant)
	status, err := repo.Status(ctx, user, &tenant, doc, false)
	if err != nil || status.Status != "pending" {
		t.Fatal("tenant status", status, err)
	}
	if _, err = repo.Status(ctx, admin, nil, doc, false); err == nil {
		t.Fatal("cross-scope status accepted")
	}
	if _, err = repo.Get(ctx, user); err == nil {
		t.Fatal("tenant read credential config")
	}
	if _, err = root.Exec(`UPDATE knowledge_embedding_profiles SET revision=repeat('b',40)`); err == nil {
		t.Fatal("mutable profile accepted")
	}
	var fingerprint string
	if err = root.QueryRow(`SELECT fingerprint FROM knowledge_embedding_profiles WHERE revision=repeat('a',40)`).Scan(&fingerprint); err != nil {
		t.Fatal(err)
	}
	if fingerprint != dtos.KnowledgeEmbeddingFingerprint(strings.Repeat("a", 40)) {
		t.Fatal("database and credential fingerprints differ")
	}
	job, err := worker.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for job.DocumentID != doc {
		exec(`UPDATE knowledge_embedding_jobs SET state='cancelled' WHERE id=$1`, job.ID)
		job, err = worker.Claim(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}
	inputs, err := worker.Read(ctx, job)
	if err != nil || len(inputs) != 1 {
		t.Fatal("read batch", len(inputs), err)
	}
	vector := "[1" + strings.Repeat(",0", 1023) + "]"
	payload := []KnowledgeEmbeddingOutput{{ChunkID: inputs[0].ChunkID, ContentHash: inputs[0].ContentHash, Embedding: vector}}
	if ok, e := worker.Commit(ctx, KnowledgeEmbeddingJob{ID: job.ID, Fence: job.Fence - 1}, payload); e != nil || ok {
		t.Fatal("stale fence", ok, e)
	}
	if ok, e := worker.Commit(ctx, job, payload); e != nil || !ok {
		t.Fatal("commit", ok, e)
	}
	status, err = repo.Status(ctx, user, &tenant, doc, false)
	if err != nil || status.IndexedChunks != 1 {
		t.Fatal("indexed status", status, err)
	}
	status, err = repo.Status(ctx, user, &tenant, doc, false)
	if err != nil || status.Status != "ready" {
		t.Fatal("ready status", status, err)
	}
	exec(`UPDATE knowledge_embedding_jobs SET state='failed' WHERE document_id=$1`, doc)
	if _, err = repo.Status(ctx, user, &tenant, doc, true); err != nil {
		t.Fatal("retry", err)
	}
	job, err = worker.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stale := job
	exec(`UPDATE knowledge_embedding_jobs SET lease_until=now()-interval '1 second' WHERE id=$1`, job.ID)
	job, err = worker.Claim(ctx)
	if err != nil || job.Fence <= stale.Fence {
		t.Fatal("expired lease recovery", err)
	}
	if ok, e := worker.Commit(ctx, stale, nil); e != nil || ok {
		t.Fatal("expired fence accepted", ok, e)
	}
	if err = worker.Fail(ctx, job, false); err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE knowledge_embedding_jobs SET next_attempt_at=now() WHERE id=$1`, job.ID)
	job, err = worker.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE knowledge_embedding_config SET generation=generation+1`)
	if ok, e := worker.Commit(ctx, job, nil); e != nil || ok {
		t.Fatal("stale config accepted", ok, e)
	}
	if _, err = workerDB.Exec(`SELECT * FROM knowledge_chunks`); err == nil {
		t.Fatal("worker direct read permitted")
	}
	exec(`DELETE FROM knowledge_documents WHERE id=$1`, doc)
	if ok, e := worker.Commit(ctx, job, payload); e != nil || ok {
		t.Fatal("deleted document accepted", ok, e)
	}
}
