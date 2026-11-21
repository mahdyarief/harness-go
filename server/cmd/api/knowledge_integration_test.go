package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hsg/internal/app"
	"hsg/internal/handlers"
	"hsg/internal/lib"
	"hsg/internal/lib/blob"
	"hsg/internal/lib/kb"
	"hsg/internal/middlewares"
	"hsg/internal/models"
	"hsg/internal/repositories"
)

// KB_TEST_DB_DSN must name a newly created disposable PostgreSQL 18 database.
// Reuse the chat migration/safety helper and the real non-owner RLS test pool.
func TestKnowledgeIntegration(t *testing.T) {
	dsn := os.Getenv("KB_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("KB_TEST_DB_DSN not set (disposable PostgreSQL 18 database)")
	}
	u, err := url.Parse(dsn)
	chatRequire(t, err)
	if !strings.HasPrefix(strings.TrimPrefix(u.Path, "/"), "hsg_chat_regression_kb_") {
		t.Fatal("refusing database without hsg_chat_regression_kb_ disposable prefix")
	}
	if (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Hostname() != "127.0.0.1" || u.Port() != "55439" || u.User.Username() != "hsg_test" {
		t.Fatal("refusing database outside disposable PostgreSQL 127.0.0.1:55439 / hsg_test")
	}
	probe, err := lib.OpenDB(dsn)
	chatRequire(t, err)
	var tables int
	err = probe.QueryRow(`SELECT count(*) FROM information_schema.tables WHERE table_schema='public'`).Scan(&tables)
	closeErr := probe.Close()
	chatRequire(t, err)
	chatRequire(t, closeErr)
	if tables != 0 {
		t.Fatal("refusing non-empty database; create a fresh disposable database for each run")
	}
	owner, _ := chatTestDB(t, "KB_TEST_DB_DSN")
	pool := tenantTestPool(t, owner, dsn)
	var runtimeRole string
	chatRequire(t, pool.QueryRow(`SELECT current_user`).Scan(&runtimeRole))
	chatExec(t, owner, `GRANT SELECT,INSERT,UPDATE,DELETE ON knowledge_documents,knowledge_chunks TO `+pgx.Identifier{runtimeRole}.Sanitize())
	// A single reused connection exposes any leaked transaction-local scope.
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)
	repo := repositories.NewKnowledgeRepository(pool)
	ctx := context.Background()
	platform, admin, member, otherAdmin, outsider := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	tenant, otherTenant := uuid.New(), uuid.New()
	for _, actor := range []uuid.UUID{platform, admin, member, otherAdmin, outsider} {
		role := "user"
		if actor == platform {
			role = "super_user"
		}
		chatExec(t, owner, `INSERT INTO users(id,name,email,role) VALUES($1,'KB Regression',$2,$3)`, actor, actor.String()+"@test.invalid", role)
	}
	for _, id := range []uuid.UUID{tenant, otherTenant} {
		chatExec(t, owner, `INSERT INTO tenants(id,name) VALUES($1,'KB Regression')`, id)
	}
	for _, m := range []struct {
		tenant, actor uuid.UUID
		role          string
	}{{tenant, admin, "tenant_admin"}, {tenant, member, "reviewer"}, {otherTenant, otherAdmin, "tenant_admin"}} {
		chatExec(t, owner, `INSERT INTO tenant_memberships(tenant_id,user_id,role) VALUES($1,$2,$3)`, m.tenant, m.actor, m.role)
	}
	tenantScope := models.ChatScope{Kind: models.ChatTenantPersonal, TenantID: tenant}
	otherScope := models.ChatScope{Kind: models.ChatTenantPersonal, TenantID: otherTenant}
	platformScope := models.ChatScope{Kind: models.ChatPlatformPersonal}
	word := "kbregression" + strings.ReplaceAll(uuid.NewString(), "-", "")
	chunks := []kb.Chunk{
		{Position: 0, HeadingPath: "Panduan > Pertama", Locator: "line:1", Content: word + " prosedur operasi bersama"},
		{Position: 1, HeadingPath: "Panduan > Kedua", Locator: "line:2", Content: "catatan tambahan tanpa kata pencarian"},
	}
	input := func(label string) repositories.KnowledgeInput {
		digest := sha256.Sum256([]byte(word + label))
		key := sha256.Sum256([]byte(uuid.NewString()))
		return repositories.KnowledgeInput{Title: "KB " + label, Filename: "panduan.txt", Format: kb.FormatText,
			SizeBytes: 200, SHA256: fmt.Sprintf("%x", digest), StorageKey: fmt.Sprintf("%x", key), CharCount: 100}
	}
	// Identical content must be accepted in different scopes, but not twice in one.
	globalIn := input("shared")
	ownIn, otherIn := globalIn, globalIn
	ownIn.StorageKey, otherIn.StorageKey = input("own-key").StorageKey, input("other-key").StorageKey
	ownIn.Title, otherIn.Title = "KB tenant one", "KB tenant two"
	create := func(actor uuid.UUID, scope *uuid.UUID, in repositories.KnowledgeInput) models.KnowledgeDocument {
		t.Helper()
		doc, err := repo.Create(ctx, actor, scope, in, chunks)
		chatRequire(t, err)
		if doc.ID == uuid.Nil || doc.UploadedBy == nil || *doc.UploadedBy != actor ||
			doc.Title != in.Title || doc.Filename != in.Filename || doc.Format != string(in.Format) ||
			doc.SizeBytes != in.SizeBytes || doc.CharCount != in.CharCount || doc.ChunkCount != len(chunks) ||
			doc.CreatedAt.IsZero() || doc.UpdatedAt.IsZero() || !kbSameScope(doc.TenantID, scope) {
			t.Fatalf("created document metadata = %+v", doc)
		}
		return doc
	}
	global := create(platform, nil, globalIn)
	own := create(admin, &tenant, ownIn)
	other := create(otherAdmin, &otherTenant, otherIn)
	list := func(t *testing.T, actor uuid.UUID, scope *uuid.UUID, want models.KnowledgeDocument) {
		t.Helper()
		docs, total, err := repo.List(ctx, actor, scope, repositories.KnowledgeListParams{ListParams: repositories.ListParams{Limit: 100, Search: want.Title}})
		chatRequire(t, err)
		if total != 1 || len(docs) != 1 || docs[0].ID != want.ID || !kbSameScope(docs[0].TenantID, scope) {
			t.Fatalf("List = %+v, total %d; want only %s", docs, total, want.ID)
		}
	}
	search := func(t *testing.T, actor uuid.UUID, scope models.ChatScope, want ...models.KnowledgeDocument) {
		t.Helper()
		hits, err := repo.Search(ctx, actor, scope, word, 10)
		chatRequire(t, err)
		expected := make(map[uuid.UUID]models.KnowledgeDocument, len(want))
		for _, doc := range want {
			expected[doc.ID] = doc
		}
		if len(hits) != len(want) {
			t.Fatalf("Search = %+v; want %d hits", hits, len(want))
		}
		for _, hit := range hits {
			doc, ok := expected[hit.DocumentID]
			if !ok || hit.Title != doc.Title || hit.Global != (doc.TenantID == nil) || hit.Score <= 0 ||
				hit.HeadingPath != chunks[0].HeadingPath || hit.Locator != chunks[0].Locator || hit.Content != chunks[0].Content {
				t.Fatalf("unexpected search hit %+v", hit)
			}
			delete(expected, hit.DocumentID)
		}
		if len(expected) != 0 {
			t.Fatalf("missing search documents: %v", expected)
		}
	}
	t.Run("list_scope_and_member_read", func(t *testing.T) {
		list(t, platform, nil, global)
		list(t, admin, &tenant, own)
		list(t, member, &tenant, own)
		list(t, otherAdmin, &otherTenant, other)
		docs, total, err := repo.List(ctx, member, &tenant, repositories.KnowledgeListParams{ListParams: repositories.ListParams{Limit: 100}})
		chatRequire(t, err)
		seen := map[uuid.UUID]bool{}
		for _, doc := range docs {
			seen[doc.ID] = true
		}
		if total != 2 || len(docs) != 2 || !seen[global.ID] || !seen[own.ID] || seen[other.ID] {
			t.Fatalf("daftar tenant harus own+global: %+v, total=%d", docs, total)
		}
	})
	t.Run("preview_visibility_pagination_and_legacy_metadata", func(t *testing.T) {
		for _, tc := range []struct {
			actor uuid.UUID
			scope *uuid.UUID
			doc   models.KnowledgeDocument
		}{{platform, nil, global}, {member, &tenant, global}, {member, &tenant, own}, {otherAdmin, &otherTenant, other}} {
			preview, err := repo.Preview(ctx, tc.actor, tc.scope, tc.doc.ID, 1, 1)
			chatRequire(t, err)
			if preview.Document.ID != tc.doc.ID || len(preview.Document.ExtractionMetadata) != 0 || len(preview.Chunks) != 1 || preview.Chunks[0].Position != 1 || preview.Chunks[0].Content != chunks[1].Content {
				t.Fatalf("pratinjau salah: %+v", preview)
			}
			preview, err = repo.Preview(ctx, tc.actor, tc.scope, tc.doc.ID, 100, 10)
			chatRequire(t, err)
			if preview.Chunks == nil || len(preview.Chunks) != 0 {
				t.Fatalf("pagination melewati akhir: %+v", preview.Chunks)
			}
		}
		for _, tc := range []struct {
			actor  uuid.UUID
			scope  *uuid.UUID
			id     uuid.UUID
			status int
		}{{platform, nil, own.ID, 404}, {member, &tenant, other.ID, 404}, {outsider, &tenant, own.ID, 403}, {member, nil, global.ID, 403}, {member, &tenant, uuid.New(), 404}} {
			_, err := repo.Preview(ctx, tc.actor, tc.scope, tc.id, 0, 10)
			chatStatus(t, err, tc.status)
		}
	})
	t.Run("search_scope_on_reused_connection", func(t *testing.T) {
		for range 2 {
			search(t, member, tenantScope, global, own)
			search(t, platform, platformScope, global)
			search(t, otherAdmin, otherScope, global, other)
			search(t, admin, tenantScope, global, own)
		}
	})
	t.Run("forbidden_writes_and_outsider_reads", func(t *testing.T) {
		for _, tc := range []struct {
			name  string
			actor uuid.UUID
			scope *uuid.UUID
			doc   uuid.UUID
		}{
			{"member tenant", member, &tenant, own.ID},
			{"outsider tenant", outsider, &tenant, own.ID},
			{"other tenant admin", otherAdmin, &tenant, own.ID},
			{"member global", member, nil, global.ID},
			{"tenant admin global", admin, nil, global.ID},
		} {
			t.Run(tc.name, func(t *testing.T) {
				chatStatus(t, repo.CheckWrite(ctx, tc.actor, tc.scope), 403)
				_, err := repo.Create(ctx, tc.actor, tc.scope, input(tc.name), chunks)
				chatStatus(t, err, 403)
				_, err = repo.Delete(ctx, tc.actor, tc.scope, tc.doc)
				chatStatus(t, err, 403)
			})
		}
		for _, actor := range []uuid.UUID{outsider, otherAdmin} {
			_, _, err := repo.List(ctx, actor, &tenant, repositories.KnowledgeListParams{ListParams: repositories.ListParams{Limit: 100}})
			chatStatus(t, err, 403)
			_, err = repo.Search(ctx, actor, tenantScope, word, 10)
			chatStatus(t, err, 403)
		}
		_, _, err := repo.List(ctx, member, nil, repositories.KnowledgeListParams{ListParams: repositories.ListParams{Limit: 100}})
		chatStatus(t, err, 403)
		_, err = repo.Search(ctx, member, platformScope, word, 10)
		chatStatus(t, err, 403)
		// Valid admins must not delete documents outside the selected scope.
		for _, tc := range []struct {
			actor uuid.UUID
			scope *uuid.UUID
			doc   uuid.UUID
		}{{admin, &tenant, global.ID}, {admin, &tenant, other.ID}, {platform, nil, own.ID}} {
			_, err := repo.Delete(ctx, tc.actor, tc.scope, tc.doc)
			chatStatus(t, err, 404)
		}
		search(t, member, tenantScope, global, own)
	})
	// Count through the owner connection: RLS must not hide leaked rows from assertions.
	counts := func(t *testing.T) [3]int {
		t.Helper()
		var n [3]int
		chatRequire(t, owner.QueryRow(`SELECT
			(SELECT count(*) FROM knowledge_documents WHERE uploaded_by IN ($1,$2,$3,$4,$5)),
			(SELECT count(*) FROM knowledge_chunks WHERE document_id IN (SELECT id FROM knowledge_documents WHERE uploaded_by IN ($1,$2,$3,$4,$5))),
			(SELECT count(*) FROM audit_logs WHERE actor_id IN ($1,$2,$3,$4,$5) AND entity_type='knowledge_document')`,
			platform, admin, member, otherAdmin, outsider).Scan(&n[0], &n[1], &n[2]))
		return n
	}
	t.Run("duplicate_uploads_conflict_without_side_effects", func(t *testing.T) {
		before := counts(t)
		for _, tc := range []struct {
			actor uuid.UUID
			scope *uuid.UUID
			in    repositories.KnowledgeInput
		}{{platform, nil, globalIn}, {admin, &tenant, ownIn}, {otherAdmin, &otherTenant, otherIn}} {
			duplicate := tc.in
			duplicate.StorageKey = input("duplicate").StorageKey
			_, err := repo.Create(ctx, tc.actor, tc.scope, duplicate, chunks)
			chatStatus(t, err, 409)
		}
		if got := counts(t); got != before || got != [3]int{3, 6, 3} {
			t.Fatalf("counts after duplicate/denied operations = %v; before %v, want [3 6 3]", got, before)
		}
	})
	t.Run("failed_chunk_rolls_back_document_chunks_and_audit", func(t *testing.T) {
		before := counts(t)
		in := input("invalid chunks")
		bad := []kb.Chunk{chunks[0], chunks[0]} // second position violates uniqueness after first insert
		_, err := repo.Create(ctx, admin, &tenant, in, bad)
		chatSQLState(t, err, "23505")
		if got := counts(t); got != before {
			t.Fatalf("partial upload persisted: counts %v, want %v", got, before)
		}
		// The rolled-back digest is reusable, not stuck as a false duplicate.
		doc := create(admin, &tenant, in)
		_, err = repo.Delete(ctx, admin, &tenant, doc.ID)
		chatRequire(t, err)
	})
	assertAudit := func(t *testing.T, doc models.KnowledgeDocument, actor uuid.UUID, role string, deleted bool) {
		t.Helper()
		want := 1
		if deleted {
			want = 2
		}
		var total, uploads, deletes int
		chatRequire(t, owner.QueryRow(`SELECT count(*),
			count(*) FILTER (WHERE action=$2 AND actor_id=$3 AND actor_role=$4
				AND tenant_id IS NOT DISTINCT FROM $5::uuid AND metadata->>'format'='txt'
				AND metadata->>'chunks'='2' AND metadata->>'size_bytes'='200'),
			count(*) FILTER (WHERE action=$6 AND actor_id=$3 AND actor_role=$4
				AND tenant_id IS NOT DISTINCT FROM $5::uuid AND metadata->>'format'='txt')
			FROM audit_logs WHERE entity_type='knowledge_document' AND entity_id=$1`, doc.ID,
			repositories.ActionKnowledgeUploaded, actor, role, doc.TenantID, repositories.ActionKnowledgeDeleted).
			Scan(&total, &uploads, &deletes))
		if total != want || uploads != 1 || deletes != want-1 {
			t.Fatalf("audit for %s: total=%d uploads=%d deletes=%d; want %d,1,%d", doc.ID, total, uploads, deletes, want, want-1)
		}
	}
	t.Run("delete_cascades_and_preserves_audit", func(t *testing.T) {
		for _, tc := range []struct {
			actor uuid.UUID
			role  string
			scope *uuid.UUID
			doc   models.KnowledgeDocument
			key   string
		}{{platform, "super_user", nil, global, globalIn.StorageKey},
			{admin, "tenant_admin", &tenant, own, ownIn.StorageKey},
			{otherAdmin, "tenant_admin", &otherTenant, other, otherIn.StorageKey}} {
			assertAudit(t, tc.doc, tc.actor, tc.role, false)
			var chunkCount int
			chatRequire(t, owner.QueryRow(`SELECT count(*) FROM knowledge_chunks WHERE document_id=$1`, tc.doc.ID).Scan(&chunkCount))
			if chunkCount != len(chunks) {
				t.Fatalf("pre-delete chunks = %d; want %d", chunkCount, len(chunks))
			}
			key, err := repo.Delete(ctx, tc.actor, tc.scope, tc.doc.ID)
			chatRequire(t, err)
			if key != tc.key {
				t.Fatalf("Delete storage key = %q; want %q", key, tc.key)
			}
			var documents int
			chatRequire(t, owner.QueryRow(`SELECT
				(SELECT count(*) FROM knowledge_documents WHERE id=$1),
				(SELECT count(*) FROM knowledge_chunks WHERE document_id=$1)`, tc.doc.ID).Scan(&documents, &chunkCount))
			if documents != 0 || chunkCount != 0 {
				t.Fatalf("delete left documents=%d chunks=%d", documents, chunkCount)
			}
			_, err = repo.Delete(ctx, tc.actor, tc.scope, tc.doc.ID)
			chatStatus(t, err, 404)
			assertAudit(t, tc.doc, tc.actor, tc.role, true)
			if tc.scope == nil {
				search(t, platform, platformScope)
				search(t, member, tenantScope, own)
				search(t, otherAdmin, otherScope, other)
			}
		}
		for _, tc := range []struct {
			actor uuid.UUID
			scope *uuid.UUID
		}{{platform, nil}, {member, &tenant}, {otherAdmin, &otherTenant}} {
			docs, total, err := repo.List(ctx, tc.actor, tc.scope, repositories.KnowledgeListParams{ListParams: repositories.ListParams{Limit: 100}})
			chatRequire(t, err)
			if total != 0 || len(docs) != 0 {
				t.Fatalf("List after delete = %+v, total=%d", docs, total)
			}
		}
		search(t, member, tenantScope)
		search(t, platform, platformScope)
		search(t, otherAdmin, otherScope)
	})
	t.Run("multipart_upload_list_search_delete", func(t *testing.T) {
		store, err := blob.NewDisk(t.TempDir())
		chatRequire(t, err)
		t.Cleanup(func() { chatRequire(t, store.Close()) })
		handler := handlers.NewKnowledgeHandler(&app.Application{TenantDB: pool}, store,
			kb.NewRegistry(kb.NewTextExtractor(), kb.NewMarkdownExtractor()))
		server := fiber.New(fiber.Config{ErrorHandler: middlewares.ErrorHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))})
		// Inject a verified identity; JWT/middleware authorization has separate HTTP tests.
		server.Use(func(c fiber.Ctx) error {
			if raw := c.Get("X-Test-Actor"); raw != "" {
				actor, err := uuid.Parse(raw)
				if err != nil {
					return err
				}
				lib.ContextSetUID(c, actor)
			}
			return c.Next()
		})
		server.Post("/tenants/:tenant_id/knowledge", handler.Upload)
		server.Get("/tenants/:tenant_id/knowledge", handler.List)
		server.Get("/tenants/:tenant_id/knowledge/:id/preview", handler.Preview)
		server.Delete("/tenants/:tenant_id/knowledge/:id", handler.Delete)
		base := "/tenants/" + tenant.String() + "/knowledge"
		content := []byte("Karyawan berhak mengajukan cuti tahunan melalui pengelola kepegawaian.")
		filename := "panduan.txt"
		call := func(method, path string, actor uuid.UUID, want int) json.RawMessage {
			t.Helper()
			var body bytes.Buffer
			contentType := ""
			if method == http.MethodPost {
				writer := multipart.NewWriter(&body)
				chatRequire(t, writer.WriteField("title", "Panduan cuti HTTP"))
				part, err := writer.CreateFormFile("file", filename)
				chatRequire(t, err)
				_, err = part.Write(content)
				chatRequire(t, err)
				chatRequire(t, writer.Close())
				contentType = writer.FormDataContentType()
			}
			request := httptest.NewRequest(method, path, &body)
			request.Header.Set("Content-Type", contentType)
			if actor != uuid.Nil {
				request.Header.Set("X-Test-Actor", actor.String())
			}
			response, err := server.Test(request)
			chatRequire(t, err)
			defer response.Body.Close()
			raw, err := io.ReadAll(response.Body)
			chatRequire(t, err)
			if response.StatusCode != want {
				t.Fatalf("%s %s: %d want %d: %s", method, path, response.StatusCode, want, raw)
			}
			var envelope struct {
				Data json.RawMessage `json:"data"`
			}
			chatRequire(t, json.Unmarshal(raw, &envelope))
			return envelope.Data
		}
		// Real multipart payloads must fail before any storage or database mutation.
		call(http.MethodPost, base, uuid.Nil, http.StatusUnauthorized)
		call(http.MethodPost, base, member, http.StatusForbidden)
		call(http.MethodPost, base, outsider, http.StatusForbidden)
		before := counts(t)
		originalContent := content
		filename = "amplifikasi.md"
		content = []byte(strings.Repeat("# h\nx\n", kb.MaxSegments+1))
		call(http.MethodPost, base, admin, http.StatusBadRequest)
		if got := counts(t); got != before {
			t.Fatalf("dokumen berlebihan mengubah database: %v; sebelum %v", got, before)
		}
		t.Run("ingest_concurrency_and_deadline", func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{}, 1)
			defer close(release)
			busy := handlers.NewKnowledgeHandler(&app.Application{TenantDB: pool}, store,
				kb.NewRegistry(kbBlockingExtractor{started: started, release: release}))
			server.Post("/busy/:tenant_id/knowledge", busy.Upload)
			busyPath := "/busy/" + tenant.String() + "/knowledge"
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			chatRequire(t, writer.WriteField("title", "Uji konkurensi"))
			part, err := writer.CreateFormFile("file", "busy.txt")
			chatRequire(t, err)
			_, err = part.Write([]byte("teks"))
			chatRequire(t, err)
			chatRequire(t, writer.Close())
			request := httptest.NewRequest(http.MethodPost, busyPath, &body)
			request.Header.Set("Content-Type", writer.FormDataContentType())
			request.Header.Set("X-Test-Actor", admin.String())
			done := make(chan error, 1)
			go func() {
				response, err := server.Test(request, fiber.TestConfig{Timeout: 5 * time.Second})
				if err == nil {
					response.Body.Close()
					if response.StatusCode != http.StatusBadRequest {
						err = fmt.Errorf("status ekstraksi: %d", response.StatusCode)
					}
				}
				done <- err
			}()
			select {
			case <-started:
			case err := <-done:
				t.Fatalf("ekstraksi tidak dimulai: %v", err)
			case <-time.After(3 * time.Second):
				t.Fatal("ekstraksi tidak dimulai")
			}
			call(http.MethodPost, busyPath, admin, http.StatusTooManyRequests)
			release <- struct{}{}
			chatRequire(t, <-done)
			if got := counts(t); got != before {
				t.Fatalf("ingest ditolak mengubah database: %v; sebelum %v", got, before)
			}
		})
		filename, content = "panduan.txt", originalContent
		var uploaded models.KnowledgeDocument
		chatRequire(t, json.Unmarshal(call(http.MethodPost, base, admin, http.StatusCreated), &uploaded))
		if uploaded.ID == uuid.Nil || uploaded.ChunkCount < 1 || uploaded.SizeBytes != int64(len(content)) {
			t.Fatalf("multipart document metadata: %+v", uploaded)
		}
		var extraction kb.ExtractionMetadata
		chatRequire(t, json.Unmarshal(uploaded.ExtractionMetadata, &extraction))
		if extraction.Status != "complete" || extraction.ExtractorVersion == "" {
			t.Fatalf("metadata ekstraksi tidak tersimpan: %s", uploaded.ExtractionMetadata)
		}
		previewPath := base + "/" + uploaded.ID.String() + "/preview"
		var preview models.KnowledgePreview
		chatRequire(t, json.Unmarshal(call(http.MethodGet, previewPath+"?limit=1", member, http.StatusOK), &preview))
		if preview.Document.ID != uploaded.ID || len(preview.Chunks) != 1 || !strings.Contains(preview.Chunks[0].Content, "cuti tahunan") || !bytes.Equal(preview.Document.ExtractionMetadata, uploaded.ExtractionMetadata) {
			t.Fatalf("pratinjau HTTP tidak sesuai: %+v", preview)
		}
		call(http.MethodGet, previewPath, outsider, http.StatusForbidden)
		call(http.MethodGet, previewPath, uuid.Nil, http.StatusUnauthorized)
		call(http.MethodGet, previewPath+"?offset=-1", member, http.StatusUnprocessableEntity)
		call(http.MethodGet, base+"/invalid/preview", member, http.StatusBadRequest)
		var key string
		chatRequire(t, owner.QueryRow(`SELECT storage_key FROM knowledge_documents WHERE id=$1`, uploaded.ID).Scan(&key))
		stored, err := store.Get(ctx, key)
		chatRequire(t, err)
		if !bytes.Equal(stored, content) {
			t.Fatal("original upload bytes changed")
		}
		call(http.MethodPost, base, admin, http.StatusConflict)
		stored, err = store.Get(ctx, key)
		chatRequire(t, err)
		if !bytes.Equal(stored, content) {
			t.Fatal("duplicate removed original blob")
		}
		var listed []models.KnowledgeDocument
		chatRequire(t, json.Unmarshal(call(http.MethodGet, base, member, http.StatusOK), &listed))
		if len(listed) != 1 || listed[0].ID != uploaded.ID {
			t.Fatalf("HTTP list: %+v", listed)
		}
		call(http.MethodGet, base, outsider, http.StatusForbidden)
		hits, err := repo.Search(ctx, member, tenantScope, "cuti tahunan", 10)
		chatRequire(t, err)
		if len(hits) != uploaded.ChunkCount || hits[0].DocumentID != uploaded.ID {
			t.Fatalf("uploaded text not searchable: %+v", hits)
		}
		call(http.MethodDelete, base+"/"+uploaded.ID.String(), member, http.StatusForbidden)
		call(http.MethodDelete, base+"/"+uploaded.ID.String(), outsider, http.StatusForbidden)
		if got := counts(t); got != [3]int{before[0] + 1, before[1] + uploaded.ChunkCount, before[2] + 1} {
			t.Fatalf("HTTP denied/duplicate operations changed row counts: %v; before %v", got, before)
		}
		call(http.MethodDelete, base+"/"+uploaded.ID.String(), admin, http.StatusOK)
		if _, err := store.Get(ctx, key); !errors.Is(err, blob.ErrNotFound) {
			t.Fatalf("blob after delete: %v", err)
		}
		if got := counts(t); got != [3]int{before[0], before[1], before[2] + 2} {
			t.Fatalf("HTTP delete failed cascade/audit: %v; before %v", got, before)
		}
		search(t, member, tenantScope)
	})
	t.Run("extraction_metadata_migration_roundtrip", func(t *testing.T) {
		in := input("migration metadata")
		in.ExtractionMetadata = json.RawMessage(`{"status":"limited","extractor_version":"test-v1","warnings":["row_limit"],"coverage":[{"unit":"rows","scope":"Sheet1","processed":20000,"total":null}]}`)
		doc := create(admin, &tenant, in)
		var got map[string]any
		chatRequire(t, json.Unmarshal(doc.ExtractionMetadata, &got))
		if got["status"] != "limited" {
			t.Fatalf("metadata hilang: %s", doc.ExtractionMetadata)
		}
		_, err := owner.Exec(`UPDATE knowledge_documents SET extraction_metadata='{"status":"invalid"}'::jsonb WHERE id=$1`, doc.ID)
		chatSQLState(t, err, "23514")
		m, err := lib.NewMigrator(owner)
		chatRequire(t, err)
		chatRequire(t, m.Steps(-1))
		var columns, docs int
		chatRequire(t, owner.QueryRow(`SELECT count(*) FROM information_schema.columns WHERE table_name='knowledge_documents' AND column_name='extraction_metadata'`).Scan(&columns))
		chatRequire(t, owner.QueryRow(`SELECT count(*) FROM knowledge_documents WHERE id=$1`, doc.ID).Scan(&docs))
		if columns != 0 || docs != 1 {
			t.Fatalf("rollback mengubah dokumen: kolom=%d dokumen=%d", columns, docs)
		}
		chatRequire(t, m.Up())
		preview, err := repo.Preview(ctx, member, &tenant, doc.ID, 0, 10)
		chatRequire(t, err)
		if len(preview.Document.ExtractionMetadata) != 0 || len(preview.Chunks) != len(chunks) {
			t.Fatalf("dokumen lama harus memiliki metadata null: %+v", preview)
		}
		_, err = repo.Delete(ctx, admin, &tenant, doc.ID)
		chatRequire(t, err)
	})
}

type kbBlockingExtractor struct {
	started chan struct{}
	release chan struct{}
}

func (kbBlockingExtractor) Format() kb.Format { return kb.FormatText }

func (e kbBlockingExtractor) Extract(ctx context.Context, _ []byte) ([]kb.Segment, error) {
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > 30*time.Second {
		return nil, errors.New("ingest tidak memiliki deadline")
	}
	close(e.started)
	select {
	case <-e.release:
		return nil, kb.ErrCorrupt
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func kbSameScope(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
