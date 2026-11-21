package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hsg/internal/lib"
	"hsg/internal/models"
	"hsg/internal/repositories"
)

func TestKnowledgeLexicalIntegration(t *testing.T) {
	dsn := os.Getenv("KB_LEXICAL_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("KB_LEXICAL_TEST_DB_DSN not set (new disposable PostgreSQL 18 database)")
	}
	u, err := url.Parse(dsn)
	chatRequire(t, err)
	if (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Hostname() != "127.0.0.1" || u.Port() == "" || !strings.HasPrefix(strings.TrimPrefix(u.Path, "/"), "hsg_chat_regression_kb_") {
		t.Fatal("refusing database outside explicit loopback PostgreSQL / hsg_chat_regression_kb_* namespace")
	}
	probe, err := lib.OpenDB(dsn)
	chatRequire(t, err)
	var tables int
	err = probe.QueryRow(`SELECT count(*) FROM information_schema.tables WHERE table_schema='public'`).Scan(&tables)
	closeErr := probe.Close()
	chatRequire(t, err)
	chatRequire(t, closeErr)
	if tables != 0 {
		t.Fatal("refusing non-empty database; create a fresh disposable database")
	}
	owner, _ := chatTestDB(t, "KB_LEXICAL_TEST_DB_DSN")
	m, err := lib.NewMigrator(owner)
	chatRequire(t, err)
	chatRequire(t, m.Migrate(20)) // Seed deployed schema to exercise the backfill.
	version, dirty, err := m.Version()
	chatRequire(t, err)
	if version != 20 || dirty {
		t.Fatalf("expected deployed schema 20, got %d dirty=%v", version, dirty)
	}
	platform, member, outsider := uuid.New(), uuid.New(), uuid.New()
	tenant, otherTenant := uuid.New(), uuid.New()
	for _, actor := range []uuid.UUID{platform, member, outsider} {
		role := "user"
		if actor == platform {
			role = "super_user"
		}
		chatExec(t, owner, `INSERT INTO users(id,name,email,role) VALUES($1,'Lexical',$2,$3)`, actor, actor.String()+"@test.invalid", role)
	}
	for _, id := range []uuid.UUID{tenant, otherTenant} {
		chatExec(t, owner, `INSERT INTO tenants(id,name) VALUES($1,'Lexical')`, id)
	}
	chatExec(t, owner, `INSERT INTO tenant_memberships(tenant_id,user_id,role) VALUES($1,$2,'tenant_admin')`, tenant, member)
	chatExec(t, owner, `INSERT INTO tenant_memberships(tenant_id,user_id,role) VALUES($1,$2,'tenant_admin')`, otherTenant, outsider)
	insert := func(scope *uuid.UUID, title, heading, content string, count int) uuid.UUID {
		t.Helper()
		id := uuid.New()
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(id.String())))
		chatExec(t, owner, `INSERT INTO knowledge_documents(id,tenant_id,title,filename,format,size_bytes,sha256,storage_key,chunk_count) VALUES($1,$2,$3,'test.txt','txt',1,$4,$4,$5)`, id, scope, title, hash, count)
		for position := 0; position < count; position++ {
			chatExec(t, owner, `INSERT INTO knowledge_chunks(document_id,tenant_id,position,heading_path,content) VALUES($1,$2,$3,$4,$5)`, id, scope, position, heading, content)
		}
		return id
	}
	global := insert(nil, "titleonlyneedle weightneedle", "", "plainbodymarker", 1)
	own := insert(&tenant, "Own", "weightneedle", "ownbodymarker", 1)
	body := insert(&tenant, "Body", "", "weightneedle bodymarker", 1)
	foreign := insert(&otherTenant, "titleonlyneedle weightneedle", "", "foreignbodymarker", 1)
	bounded := insert(nil, "boundedneedle", "", "identical passage", 55)
	chatRequire(t, m.Steps(1)) // Exercise the title backfill from version 20.
	chatRequire(t, m.Up())     // Runtime role checks require the embedding schema too.
	pool := tenantTestPool(t, owner, dsn)
	var runtimeRole string
	chatRequire(t, pool.QueryRow(`SELECT current_user`).Scan(&runtimeRole))
	chatExec(t, owner, `GRANT SELECT,INSERT,UPDATE,DELETE ON knowledge_documents,knowledge_chunks TO `+pgx.Identifier{runtimeRole}.Sanitize())
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)
	repo := repositories.NewKnowledgeRepository(pool)
	ctx := context.Background()
	tenantScope := models.ChatScope{Kind: models.ChatTenantPersonal, TenantID: tenant}
	platformScope := models.ChatScope{Kind: models.ChatPlatformPersonal}
	search := func(actor uuid.UUID, scope models.ChatScope, query string, limit int) []models.KnowledgeHit {
		t.Helper()
		hits, err := repo.SearchCandidates(ctx, actor, scope, query, limit)
		chatRequire(t, err)
		return hits
	}
	t.Run("backfill and weighted ranking", func(t *testing.T) {
		hits := search(member, tenantScope, "weightneedle", 50)
		if len(hits) != 3 || hits[0].DocumentID != global || hits[1].DocumentID != own || hits[2].DocumentID != body || !(hits[0].Score > hits[1].Score && hits[1].Score > hits[2].Score) {
			t.Fatalf("weighted candidates = %+v", hits)
		}
		for _, hit := range hits {
			if hit.ChunkID == uuid.Nil || hit.Position != 0 || hit.ContentHash != fmt.Sprintf("%x", sha256.Sum256([]byte(hit.Content))) {
				t.Fatalf("candidate provenance = %+v", hit)
			}
		}
		if hits := search(member, tenantScope, "titleonlyneedle", 50); len(hits) != 1 || hits[0].DocumentID != global || !hits[0].Global {
			t.Fatalf("title-only search = %+v", hits)
		}
	})
	t.Run("scope authorization and raw RLS", func(t *testing.T) {
		if hits := search(platform, platformScope, "weightneedle", 50); len(hits) != 1 || hits[0].DocumentID != global {
			t.Fatalf("platform results = %+v", hits)
		}
		otherScope := models.ChatScope{Kind: models.ChatTenantPersonal, TenantID: otherTenant}
		if hits := search(outsider, otherScope, "weightneedle", 50); len(hits) != 2 {
			t.Fatalf("other tenant results = %+v", hits)
		}
		for _, attempt := range []struct {
			actor uuid.UUID
			scope models.ChatScope
		}{{member, platformScope}, {outsider, tenantScope}, {platform, tenantScope}} {
			if _, err := repo.SearchCandidates(ctx, attempt.actor, attempt.scope, "weightneedle", 50); err == nil {
				t.Fatal("unproven scope accepted")
			}
		}
		tx, err := pool.BeginTx(ctx, nil)
		chatRequire(t, err)
		defer tx.Rollback()
		_, err = tx.ExecContext(ctx, `SELECT set_config('app.user_id',$1,true),set_config('app.tenant_id',$2,true)`, member.String(), tenant.String())
		chatRequire(t, err)
		var count int
		chatRequire(t, tx.QueryRowContext(ctx, `SELECT count(*) FROM knowledge_chunks WHERE document_id=$1`, foreign).Scan(&count))
		if count != 0 {
			t.Fatal("raw chunk RLS leaked foreign tenant")
		}
		chatRequire(t, tx.QueryRowContext(ctx, `SELECT count(*) FROM knowledge_documents WHERE id=$1`, foreign).Scan(&count))
		if count != 0 {
			t.Fatal("raw document RLS leaked foreign tenant")
		}
	})
	t.Run("bounds deterministic order and compatibility", func(t *testing.T) {
		hits := search(member, tenantScope, "boundedneedle", 1000)
		if len(hits) != 50 {
			t.Fatalf("candidate cap = %d", len(hits))
		}
		for i, hit := range hits {
			if hit.DocumentID != bounded || hit.Position != i {
				t.Fatalf("tie order = %+v", hit)
			}
		}
		if again := search(member, tenantScope, "boundedneedle", 50); !reflect.DeepEqual(hits, again) {
			t.Fatal("nondeterministic candidates")
		}
		for _, limit := range []int{0, -1, 11, 50} {
			legacy, err := repo.Search(ctx, member, tenantScope, "boundedneedle", limit)
			chatRequire(t, err)
			if len(legacy) != 1 {
				t.Fatalf("deduplicated limit %d = %d", limit, len(legacy))
			}
		}
	})
	t.Run("search diversifies unique chunks and exposes ranking signal", func(t *testing.T) {
		doc := insert(&tenant, "diversifyneedle", "", "distinct passage ", 4)
		chatExec(t, owner, `UPDATE knowledge_chunks SET content=content || position::text WHERE document_id=$1`, doc)
		hits, err := repo.Search(ctx, member, tenantScope, "diversifyneedle", 5)
		chatRequire(t, err)
		if len(hits) != 4 || hits[0].Position != 0 || hits[1].Position != 1 || hits[2].Position != 2 || hits[3].Position != 3 {
			t.Fatalf("document diversification: %+v", hits)
		}
		if hits[0].Score != 1.0/61 || hits[1].Score != 1.0/62 {
			t.Fatalf("expected one-based RRF signal, not confidence: %+v", hits)
		}
	})
	t.Run("safe query semantics", func(t *testing.T) {
		for _, query := range []string{"", " \t\n ", "dan yang di", "\""} {
			if hits := search(member, tenantScope, query, 50); len(hits) != 0 {
				t.Fatalf("empty/stopword query %q = %+v", query, hits)
			}
		}
		if hits := search(member, tenantScope, "\n weightneedle\t -bodymarker  ", 50); len(hits) != 2 {
			t.Fatalf("negation = %+v", hits)
		}
		code := insert(nil, "English ICD10 ZXQ482", "", "exact phrase 4817", 1)
		for _, query := range []string{"ICD10", "ZXQ482", "\"exact\tphrase\" 4817"} {
			if hits := search(platform, platformScope, query, 50); len(hits) != 1 || hits[0].DocumentID != code {
				t.Fatalf("exact code/phrase %q = %+v", query, hits)
			}
		}
	})
	t.Run("role and membership revalidated on every search", func(t *testing.T) {
		chatExec(t, owner, `UPDATE users SET role='user' WHERE id=$1`, platform)
		if _, err := repo.SearchCandidates(ctx, platform, platformScope, "weightneedle", 50); err == nil {
			t.Fatal("revoked platform role retained global search")
		}
		chatExec(t, owner, `UPDATE users SET role='super_user' WHERE id=$1`, platform)
		chatExec(t, owner, `UPDATE tenant_memberships SET role='tenant_admin' WHERE tenant_id=$1 AND user_id=$2`, tenant, member)
		if hits := search(member, tenantScope, "weightneedle", 50); len(hits) != 3 {
			t.Fatalf("updated member role lost access: %+v", hits)
		}
		chatExec(t, owner, `DELETE FROM tenant_memberships WHERE tenant_id=$1 AND user_id=$2`, tenant, member)
		if _, err := repo.SearchCandidates(ctx, member, tenantScope, "weightneedle", 50); err == nil {
			t.Fatal("revoked membership retained search access")
		}
		chatExec(t, owner, `INSERT INTO tenant_memberships(tenant_id,user_id,role) VALUES($1,$2,'reviewer')`, tenant, member)
		if hits := search(member, tenantScope, "weightneedle", 50); len(hits) != 3 {
			t.Fatalf("restored membership did not restore access: %+v", hits)
		}
		chatExec(t, owner, `UPDATE tenant_memberships SET role='tenant_admin' WHERE tenant_id=$1 AND user_id=$2`, tenant, member)
	})
	t.Run("rename sync insert projection and cascade", func(t *testing.T) {
		tx, err := pool.BeginTx(ctx, nil)
		chatRequire(t, err)
		defer tx.Rollback()
		_, err = tx.ExecContext(ctx, `SELECT set_config('app.user_id',$1,true),set_config('app.tenant_id',$2,true)`, member.String(), tenant.String())
		chatRequire(t, err)
		_, err = tx.ExecContext(ctx, `UPDATE knowledge_documents SET title='renamedneedle' WHERE id=$1`, own)
		chatRequire(t, err)
		_, err = tx.ExecContext(ctx, `INSERT INTO knowledge_chunks(document_id,tenant_id,position,title,content) VALUES($1,$2,1,'forgedneedle','new passage')`, own, tenant)
		chatRequire(t, err)
		chatRequire(t, tx.Commit())
		if hits := search(member, tenantScope, "renamedneedle", 50); len(hits) != 2 {
			t.Fatalf("rename/insert title = %+v", hits)
		}
		if hits := search(member, tenantScope, "forgedneedle", 50); len(hits) != 0 {
			t.Fatal("client-controlled title projection")
		}
		chatExec(t, owner, `UPDATE knowledge_documents SET title='replacementneedle' WHERE id=$1`, own)
		if hits := search(member, tenantScope, "renamedneedle", 50); len(hits) != 0 {
			t.Fatal("stale renamed title")
		}
		_, err = repo.Delete(ctx, member, &tenant, own)
		chatRequire(t, err)
		var count int
		chatRequire(t, owner.QueryRow(`SELECT count(*) FROM knowledge_chunks WHERE document_id=$1`, own).Scan(&count))
		if count != 0 {
			t.Fatal("document cascade retained chunks")
		}
	})
	t.Run("down up preserves rows and restores vectors", func(t *testing.T) {
		var documents, chunks int
		chatRequire(t, owner.QueryRow(`SELECT count(*) FROM knowledge_documents`).Scan(&documents))
		chatRequire(t, owner.QueryRow(`SELECT count(*) FROM knowledge_chunks`).Scan(&chunks))
		chatRequire(t, m.Migrate(20))
		var same bool
		chatRequire(t, owner.QueryRow(`SELECT bool_and(search = (setweight(to_tsvector('indonesian'::regconfig,heading_path),'A') || setweight(to_tsvector('indonesian'::regconfig,content),'B'))) FROM knowledge_chunks`).Scan(&same))
		if !same {
			t.Fatal("down did not restore heading/content vector")
		}
		if hits := search(platform, platformScope, "titleonlyneedle", 50); len(hits) != 0 {
			t.Fatal("title remained searchable after down")
		}
		chatExec(t, owner, `UPDATE knowledge_documents SET title='reappliedneedle' WHERE id=$1`, global)
		chatRequire(t, m.Steps(1))
		if hits := search(platform, platformScope, "reappliedneedle", 50); len(hits) != 1 || hits[0].DocumentID != global {
			t.Fatalf("reapplied backfill = %+v", hits)
		}
		var actualDocuments, actualChunks int
		chatRequire(t, owner.QueryRow(`SELECT count(*) FROM knowledge_documents`).Scan(&actualDocuments))
		chatRequire(t, owner.QueryRow(`SELECT count(*) FROM knowledge_chunks`).Scan(&actualChunks))
		if documents != actualDocuments || chunks != actualChunks {
			t.Fatalf("down/up lost rows: documents %d/%d chunks %d/%d", documents, actualDocuments, chunks, actualChunks)
		}
		chatRequire(t, owner.QueryRow(`SELECT bool_and(c.title=d.title AND c.search = (setweight(to_tsvector('indonesian'::regconfig,d.title),'A') || setweight(to_tsvector('indonesian'::regconfig,c.heading_path),'B') || setweight(to_tsvector('indonesian'::regconfig,c.content),'D'))) FROM knowledge_chunks c JOIN knowledge_documents d ON d.id=c.document_id`).Scan(&same))
		if !same {
			t.Fatal("reapplied title projection/vector mismatch")
		}
	})
}
