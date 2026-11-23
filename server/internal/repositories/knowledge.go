package repositories

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"hsg/internal/lib/apperr"
	"hsg/internal/lib/kb"
	"hsg/internal/models"
)

// KnowledgeRepository owns knowledge documents and their searchable chunks.
//
// Scope is explicit on every call: a nil tenant means the global library
// (super user only), otherwise that tenant's own library. Writes are proven
// here (super_user from users.role, tenant_admin from membership) and enforced
// again by the row-level security policies, so a bug in either layer does not
// widen access on its own.
type KnowledgeRepository struct{ db *sql.DB }

func NewKnowledgeRepository(db *sql.DB) *KnowledgeRepository { return &KnowledgeRepository{db: db} }

const knowledgeColumns = `id,tenant_id,category_id,(SELECT name FROM knowledge_categories WHERE id = knowledge_documents.category_id),title,filename,format,size_bytes,char_count,chunk_count,uploaded_by,created_at,updated_at,extraction_metadata,attributes,valid_from,valid_to,version,status`

// knowledgeListColumns adds the category label through a LEFT JOIN, so a
// document without a category reports a NULL name instead of an inner join
// silently dropping it.
const knowledgeListColumns = `d.id,d.tenant_id,d.category_id,c.name,d.title,d.filename,d.format,d.size_bytes,d.char_count,d.chunk_count,d.uploaded_by,d.created_at,d.updated_at,d.extraction_metadata,d.attributes,d.valid_from,d.valid_to,d.version,d.status`

// KnowledgeInput menyimpan hasil ekstraksi. StorageKey unik per percobaan
// unggah sehingga rollback tidak menghapus blob milik transaksi lain.
type KnowledgeInput struct {
	Title              string
	Filename           string
	Format             kb.Format
	SizeBytes          int64
	SHA256             string
	StorageKey         string
	CharCount          int
	ExtractionMetadata json.RawMessage
	// Attributes, ValidFrom, ValidTo, and Version are retrieval metadata:
	// they narrow search and travel on every citation. They never gate access.
	Attributes json.RawMessage
	ValidFrom  *time.Time
	ValidTo    *time.Time
	Version    string
	CategoryID *uuid.UUID
	Status     string
}

// KnowledgeCategoryInput is the create payload for a category.
type KnowledgeCategoryInput struct {
	Name string
}

// KnowledgeListParams extends ListParams with the optional category facet.
type KnowledgeListParams struct {
	ListParams
	CategoryID    *uuid.UUID
	Uncategorized bool
}

// begin opens a transaction that proves the actor may act on the scope.
func (r *KnowledgeRepository) begin(ctx context.Context, actor uuid.UUID, tenant *uuid.UUID, write bool) (*sql.Tx, string, error) {
	if r == nil || r.db == nil || actor == uuid.Nil {
		return nil, "", apperr.ErrForbidden
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, "", err
	}
	fail := func(err error) (*sql.Tx, string, error) {
		tx.Rollback()
		return nil, "", err
	}
	unsafe, err := TenantRoleUnsafe(ctx, tx)
	if err != nil {
		return fail(err)
	}
	if unsafe {
		return fail(apperr.ErrForbidden)
	}
	if _, err = tx.ExecContext(ctx, `SELECT set_config('app.tenant_id','',true), set_config('app.user_id',$1,true)`, actor.String()); err != nil {
		return fail(err)
	}
	if tenant == nil {
		if err = platformAdmin(ctx, tx, actor); err != nil {
			return fail(err)
		}
		return tx, "super_user", nil
	}
	if err = verified(ctx, tx, actor, *tenant, write); err != nil {
		return fail(err)
	}
	if write {
		return tx, "tenant_admin", nil
	}
	return tx, "member", nil
}

var ErrKnowledgeCommitUnknown = errors.New("knowledge: commit outcome unknown")

func (r *KnowledgeRepository) CheckWrite(ctx context.Context, actor uuid.UUID, tenant *uuid.UUID) error {
	tx, _, err := r.begin(ctx, actor, tenant, true)
	if err != nil {
		return err
	}
	return tx.Rollback()
}

func scanKnowledgeDocument(row rowScanner) (models.KnowledgeDocument, error) {
	var d models.KnowledgeDocument
	var metadata, attributes []byte
	err := row.Scan(&d.ID, &d.TenantID, &d.CategoryID, &d.CategoryName, &d.Title, &d.Filename, &d.Format, &d.SizeBytes, &d.CharCount, &d.ChunkCount, &d.UploadedBy, &d.CreatedAt, &d.UpdatedAt, &metadata, &attributes, &d.ValidFrom, &d.ValidTo, &d.Version, &d.Status)
	d.ExtractionMetadata = metadata
	d.Attributes = attributes
	return d, err
}

func scanKnowledgeCategory(row rowScanner) (models.KnowledgeCategory, error) {
	var c models.KnowledgeCategory
	var tenant uuid.NullUUID
	err := row.Scan(&c.ID, &tenant, &c.Name, &c.Slug, &c.CreatedAt, &c.UpdatedAt)
	if tenant.Valid {
		c.TenantID = &tenant.UUID
	}
	return c, err
}

const knowledgeCategoryColumns = `id,tenant_id,name,slug,created_at,updated_at`

var knowledgeCategoryOrder = map[string]string{"name": "name", "created_at": "created_at"}

// Create stores a document and all of its chunks in one transaction, so a
// document is either fully searchable or absent.
func (r *KnowledgeRepository) Create(ctx context.Context, actor uuid.UUID, tenant *uuid.UUID, in KnowledgeInput, chunks []kb.Chunk) (models.KnowledgeDocument, error) {
	if len(chunks) == 0 {
		return models.KnowledgeDocument{}, apperr.New(apperr.KindBadRequest, "Dokumen tidak berisi teks yang dapat diindeks")
	}
	tx, role, err := r.begin(ctx, actor, tenant, true)
	if err != nil {
		return models.KnowledgeDocument{}, err
	}
	defer tx.Rollback()

	var catID any
	if in.CategoryID != nil {
		catID = in.CategoryID
	}
	if len(in.Attributes) == 0 {
		in.Attributes = json.RawMessage(`{}`)
	}
	doc, err := scanKnowledgeDocument(tx.QueryRowContext(ctx,
		`INSERT INTO knowledge_documents (tenant_id,category_id,title,filename,format,size_bytes,sha256,storage_key,char_count,chunk_count,uploaded_by,extraction_metadata,attributes,valid_from,valid_to,version,status)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17) RETURNING `+knowledgeColumns,
		tenant, catID, in.Title, in.Filename, string(in.Format), in.SizeBytes, in.SHA256, in.StorageKey, in.CharCount, len(chunks), actor, in.ExtractionMetadata, in.Attributes, in.ValidFrom, in.ValidTo, in.Version, knowledgeStatus(in.Status)))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return models.KnowledgeDocument{}, apperr.New(apperr.KindConflict, "Dokumen yang sama sudah diunggah")
		}
		return models.KnowledgeDocument{}, err
	}

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO knowledge_chunks (document_id,tenant_id,position,heading_path,locator,content) VALUES ($1,$2,$3,$4,$5,$6)`)
	if err != nil {
		return models.KnowledgeDocument{}, err
	}
	defer stmt.Close()
	for _, c := range chunks {
		if _, err = stmt.ExecContext(ctx, doc.ID, tenant, c.Position, c.HeadingPath, c.Locator, c.Content); err != nil {
			return models.KnowledgeDocument{}, fmt.Errorf("insert knowledge chunk %d: %w", c.Position, err)
		}
	}

	if err = auditAppend(ctx, tx, actor, role, ActionKnowledgeUploaded, "knowledge_document", &doc.ID, tenant,
		map[string]any{"format": doc.Format, "size_bytes": doc.SizeBytes, "chunks": doc.ChunkCount}); err != nil {
		return models.KnowledgeDocument{}, err
	}
	if err = tx.Commit(); err != nil {
		return models.KnowledgeDocument{}, fmt.Errorf("%w: %w", ErrKnowledgeCommitUnknown, err)
	}
	return doc, nil
}

var knowledgeOrder = map[string]string{"created_at": "created_at", "title": "title", "size": "size_bytes"}

// List menampilkan dokumen global serta dokumen tenant aktif, terbaru lebih dulu.
func (r *KnowledgeRepository) List(ctx context.Context, actor uuid.UUID, tenant *uuid.UUID, p KnowledgeListParams) ([]models.KnowledgeDocument, int, error) {
	tx, _, err := r.begin(ctx, actor, tenant, false)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()

	w := newWhere("TRUE")
	if tenant == nil {
		w.conds = append(w.conds, "d.tenant_id IS NULL")
	} else {
		w.add("(d.tenant_id = $%d OR d.tenant_id IS NULL)", *tenant)
	}
	if p.CategoryID != nil {
		w.add("d.category_id = $%d", *p.CategoryID)
	} else if p.Uncategorized {
		w.conds = append(w.conds, "d.category_id IS NULL")
	}
	w.search(p.Search, "d.title", "d.filename")
	where, args, limit, offset := w.page(p.ListParams)

	var total int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM knowledge_documents d WHERE `+where, args[:len(args)-2]...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.QueryContext(ctx,
		fmt.Sprintf(`SELECT %s FROM knowledge_documents d
			LEFT JOIN knowledge_categories c ON c.id = d.category_id
			WHERE %s ORDER BY %s, d.id LIMIT $%d OFFSET $%d`,
			knowledgeListColumns, where, resolveOrder(p.Sorted, knowledgeOrder, "created_at DESC"), limit, offset), args...)
	if err != nil {
		return nil, 0, err
	}
	docs, err := collectRows(rows, scanKnowledgeDocument)
	if err != nil {
		return nil, 0, err
	}
	return docs, total, tx.Commit()
}

// Delete removes the document and, through the cascade, its chunks. It returns
// the storage key so the caller can drop the blob; keys are scope-specific, so
// no other row can share it.
func (r *KnowledgeRepository) Preview(ctx context.Context, actor uuid.UUID, tenant *uuid.UUID, id uuid.UUID, offset, limit int) (models.KnowledgePreview, error) {
	tx, _, err := r.begin(ctx, actor, tenant, false)
	if err != nil {
		return models.KnowledgePreview{}, err
	}
	defer tx.Rollback()
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 20 {
		limit = 20
	}
	// One statement reads the document and its page of chunks from a single
	// snapshot, so a concurrent delete cannot return a document with no chunks.
	rows, err := tx.QueryContext(ctx, `SELECT d.id,d.tenant_id,d.category_id,cat.name,d.title,d.filename,d.format,d.size_bytes,d.char_count,d.chunk_count,d.uploaded_by,d.created_at,d.updated_at,d.extraction_metadata,d.status,c.position,c.heading_path,c.locator,c.content
		FROM knowledge_documents d
		LEFT JOIN knowledge_categories cat ON cat.id = d.category_id
		LEFT JOIN LATERAL (
			SELECT position,heading_path,locator,content
			FROM knowledge_chunks
			WHERE document_id=d.id
			ORDER BY position
			LIMIT $3 OFFSET $4
		) c ON TRUE
		WHERE d.id=$1 AND (d.tenant_id=$2::uuid OR d.tenant_id IS NULL)
		ORDER BY c.position`, id, tenant, limit, offset)
	if err != nil {
		return models.KnowledgePreview{}, err
	}
	defer rows.Close()
	var (
		preview models.KnowledgePreview
		found   bool
	)
	preview.Chunks = make([]models.KnowledgePreviewChunk, 0)
	for rows.Next() {
		var d models.KnowledgeDocument
		var metadata []byte
		var position sql.NullInt64
		var headingPath, locator, content sql.NullString
		if err := rows.Scan(&d.ID, &d.TenantID, &d.CategoryID, &d.CategoryName, &d.Title, &d.Filename, &d.Format, &d.SizeBytes,
			&d.CharCount, &d.ChunkCount, &d.UploadedBy, &d.CreatedAt, &d.UpdatedAt, &metadata, &d.Status,
			&position, &headingPath, &locator, &content); err != nil {
			return models.KnowledgePreview{}, err
		}
		if !found {
			d.ExtractionMetadata = metadata
			preview.Document = d
			found = true
		}
		if position.Valid {
			preview.Chunks = append(preview.Chunks, models.KnowledgePreviewChunk{
				Position:    int(position.Int64),
				HeadingPath: headingPath.String,
				Locator:     locator.String,
				Content:     content.String,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return models.KnowledgePreview{}, err
	}
	if !found {
		return models.KnowledgePreview{}, translateNotFound("knowledge document", sql.ErrNoRows)
	}
	return preview, tx.Commit()
}

func (r *KnowledgeRepository) Delete(ctx context.Context, actor uuid.UUID, tenant *uuid.UUID, id uuid.UUID) (string, error) {
	tx, role, err := r.begin(ctx, actor, tenant, true)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	var key, format string
	err = tx.QueryRowContext(ctx,
		`DELETE FROM knowledge_documents WHERE id=$1 AND tenant_id IS NOT DISTINCT FROM $2::uuid RETURNING storage_key, format`,
		id, tenant).Scan(&key, &format)
	if err != nil {
		return "", translateNotFound("knowledge document", err)
	}
	if err = auditAppend(ctx, tx, actor, role, ActionKnowledgeDeleted, "knowledge_document", &id, tenant,
		map[string]any{"format": format}); err != nil {
		return "", err
	}
	return key, tx.Commit()
}

// SetCategory moves a document between categories (or out of one, with a nil
// id). The trigger rejects a category that does not share the document scope.
func (r *KnowledgeRepository) SetCategory(ctx context.Context, actor uuid.UUID, tenant *uuid.UUID, id uuid.UUID, categoryID *uuid.UUID) (models.KnowledgeDocument, error) {
	tx, _, err := r.begin(ctx, actor, tenant, true)
	if err != nil {
		return models.KnowledgeDocument{}, err
	}
	defer tx.Rollback()

	var catID any
	if categoryID != nil {
		catID = categoryID
	}
	doc, err := scanKnowledgeDocument(tx.QueryRowContext(ctx,
		`UPDATE knowledge_documents
		   SET category_id = $1, updated_at = now()
		 WHERE id=$2 AND tenant_id IS NOT DISTINCT FROM $3::uuid
		 RETURNING `+knowledgeColumns, catID, id, tenant))
	if err != nil {
		return models.KnowledgeDocument{}, translateNotFound("knowledge document", err)
	}
	return doc, tx.Commit()
}

// knowledgeStatus menjaga pemanggil lama (seed, fixture integrasi) tetap
// menghasilkan dokumen aktif bila tidak mengirim status eksplisit.
func knowledgeStatus(status string) string {
	if status == "" {
		return "published"
	}
	return status
}

// SetStatus menerbitkan dokumen atau mengembalikannya ke draft. Review
// mengubah status lewat sini; retrieval agen hanya membaca status published.
func (r *KnowledgeRepository) SetStatus(ctx context.Context, actor uuid.UUID, tenant *uuid.UUID, id uuid.UUID, status string) (models.KnowledgeDocument, error) {
	tx, role, err := r.begin(ctx, actor, tenant, true)
	if err != nil {
		return models.KnowledgeDocument{}, err
	}
	defer tx.Rollback()
	doc, err := scanKnowledgeDocument(tx.QueryRowContext(ctx,
		`UPDATE knowledge_documents
		   SET status = $1, updated_at = now()
		 WHERE id=$2 AND tenant_id IS NOT DISTINCT FROM $3::uuid
		 RETURNING `+knowledgeColumns, status, id, tenant))
	if err != nil {
		return models.KnowledgeDocument{}, translateNotFound("knowledge document", err)
	}
	action := ActionKnowledgeUnpublished
	if status == "published" {
		action = ActionKnowledgePublished
	}
	if err = auditAppend(ctx, tx, actor, role, action, "knowledge_document", &doc.ID, tenant,
		map[string]any{"status": status}); err != nil {
		return models.KnowledgeDocument{}, err
	}
	return doc, tx.Commit()
}

// ListCategories returns every category in the library, name-ordered.
func (r *KnowledgeRepository) ListCategories(ctx context.Context, actor uuid.UUID, tenant *uuid.UUID, p ListParams) ([]models.KnowledgeCategory, int, error) {
	tx, _, err := r.begin(ctx, actor, tenant, false)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()

	w := newWhere("TRUE")
	if tenant == nil {
		w.conds = append(w.conds, "tenant_id IS NULL")
	} else {
		w.add("(tenant_id = $%d OR tenant_id IS NULL)", *tenant)
	}
	w.search(p.Search, "name")
	where, args, limit, offset := w.page(p)

	var total int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM knowledge_categories WHERE `+where, args[:len(args)-2]...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.QueryContext(ctx,
		fmt.Sprintf(`SELECT %s FROM knowledge_categories WHERE %s ORDER BY %s, id LIMIT $%d OFFSET $%d`,
			knowledgeCategoryColumns, where, resolveOrder(p.Sorted, knowledgeCategoryOrder, "name ASC"), limit, offset), args...)
	if err != nil {
		return nil, 0, err
	}
	cats, err := collectRows(rows, scanKnowledgeCategory)
	if err != nil {
		return nil, 0, err
	}
	return cats, total, tx.Commit()
}

// CreateCategory inserts a category after folding its name into a slug. The
// unique (tenant_id, slug) constraint turns duplicates into a conflict.
func (r *KnowledgeRepository) CreateCategory(ctx context.Context, actor uuid.UUID, tenant *uuid.UUID, in KnowledgeCategoryInput) (models.KnowledgeCategory, error) {
	name := strings.TrimSpace(in.Name)
	if len([]rune(name)) < 1 || len([]rune(name)) > 80 {
		return models.KnowledgeCategory{}, apperr.New(apperr.KindBadRequest, "Nama kategori harus 1–80 karakter")
	}
	tx, _, err := r.begin(ctx, actor, tenant, true)
	if err != nil {
		return models.KnowledgeCategory{}, err
	}
	defer tx.Rollback()

	slug := slugifyCategory(name)
	var id uuid.UUID
	if id, err = uuid.NewV7(); err != nil {
		return models.KnowledgeCategory{}, err
	}
	var c models.KnowledgeCategory
	var t uuid.NullUUID
	err = tx.QueryRowContext(ctx,
		`INSERT INTO knowledge_categories (id,tenant_id,name,slug) VALUES ($1,$2,$3,$4)
		 RETURNING `+knowledgeCategoryColumns, id, tenant, name, slug).
		Scan(&c.ID, &t, &c.Name, &c.Slug, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return models.KnowledgeCategory{}, apperr.New(apperr.KindConflict, "Kategori dengan nama tersebut sudah ada")
		}
		return models.KnowledgeCategory{}, err
	}
	if t.Valid {
		c.TenantID = &t.UUID
	}
	return c, tx.Commit()
}

// RenameCategory updates a category name and its derived slug. The scope is
// immutable (trigger), so the tenant_id in the RETURNING is unchanged.
func (r *KnowledgeRepository) RenameCategory(ctx context.Context, actor uuid.UUID, tenant *uuid.UUID, id uuid.UUID, name string) (models.KnowledgeCategory, error) {
	name = strings.TrimSpace(name)
	if len([]rune(name)) < 1 || len([]rune(name)) > 80 {
		return models.KnowledgeCategory{}, apperr.New(apperr.KindBadRequest, "Nama kategori harus 1–80 karakter")
	}
	tx, _, err := r.begin(ctx, actor, tenant, true)
	if err != nil {
		return models.KnowledgeCategory{}, err
	}
	defer tx.Rollback()

	var c models.KnowledgeCategory
	var t uuid.NullUUID
	err = tx.QueryRowContext(ctx,
		`UPDATE knowledge_categories SET name=$1, slug=$2, updated_at=now()
		 WHERE id=$3 AND tenant_id IS NOT DISTINCT FROM $4::uuid
		 RETURNING `+knowledgeCategoryColumns, name, slugifyCategory(name), id, tenant).
		Scan(&c.ID, &t, &c.Name, &c.Slug, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return models.KnowledgeCategory{}, apperr.New(apperr.KindConflict, "Kategori dengan nama tersebut sudah ada")
		}
		return models.KnowledgeCategory{}, translateNotFound("knowledge category", err)
	}
	if t.Valid {
		c.TenantID = &t.UUID
	}
	return c, tx.Commit()
}

// DeleteCategory removes a category; documents keep their rows and fall back to
// "Tanpa kategori" through the ON DELETE SET NULL foreign key.
func (r *KnowledgeRepository) DeleteCategory(ctx context.Context, actor uuid.UUID, tenant *uuid.UUID, id uuid.UUID) error {
	tx, _, err := r.begin(ctx, actor, tenant, true)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`DELETE FROM knowledge_categories WHERE id=$1 AND tenant_id IS NOT DISTINCT FROM $2::uuid`, id, tenant)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return translateNotFound("knowledge category", sql.ErrNoRows)
	}
	return tx.Commit()
}

// slugifyCategory folds a category name to a stable, comparable slug: lowercase,
// accents stripped, runs of non-alphanumeric collapsed to a single hyphen.
func slugifyCategory(name string) string {
	normalized := strings.ToLower(name)
	var b strings.Builder
	prevDash := false
	for _, r := range normalized {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		case r == ' ' || r == '-' || r == '_' || r == '.' || r == '/' || r == ',':
			if !prevDash && b.Len() > 0 {
				b.WriteRune('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// Search finds passages for the agent. Visibility comes entirely from the
// session scope the chat transaction pins: a tenant chat sees its own library
// plus the global one, a platform chat sees the global library only. Neither
// the model nor the request can name a tenant.
func (r *KnowledgeRepository) Search(ctx context.Context, user uuid.UUID, scope models.ChatScope, query string, limit int, filters ...models.KnowledgeFilter) ([]models.KnowledgeHit, error) {
	if limit <= 0 || limit > kb.MaxPassages {
		limit = kb.MaxPassages
	}
	var filter models.KnowledgeFilter
	if len(filters) > 0 {
		filter = filters[0]
	}
	hits, err := r.SearchCandidates(ctx, user, scope, query, kb.MaxBranchCandidates, filter)
	if err != nil {
		return nil, err
	}
	candidates := make([]kb.Candidate, 0, len(hits))
	byID := make(map[string]models.KnowledgeHit, len(hits))
	for _, hit := range hits {
		id := hit.ChunkID.String()
		byID[id] = hit
		candidates = append(candidates, kb.Candidate{
			ChunkID: id, DocumentID: hit.DocumentID.String(), Position: hit.Position,
			Content: hit.Content, ContentHash: hit.ContentHash,
		})
	}
	selected := kb.SelectCandidates(kb.WeightedRRF(candidates, nil, 1, 0), limit)
	result := make([]models.KnowledgeHit, 0, len(selected))
	for _, candidate := range selected {
		hit := byID[candidate.ChunkID]
		hit.Score = candidate.Score // Ranking signal, not confidence.
		result = append(result, hit)
	}
	return result, nil
}

// SearchCandidates returns a bounded lexical shortlist in the server-proven
// chat scope. It keeps websearch phrase/negation semantics without expansion.
func (r *KnowledgeRepository) SearchCandidates(ctx context.Context, user uuid.UUID, scope models.ChatScope, query string, limit int, filters ...models.KnowledgeFilter) ([]models.KnowledgeHit, error) {
	if r == nil || r.db == nil {
		return nil, apperr.ErrForbidden
	}
	if limit <= 0 {
		limit = kb.MaxBranchCandidates
	} else if limit > kb.MaxBranchCandidates {
		limit = kb.MaxBranchCandidates
	}
	var filter models.KnowledgeFilter
	if len(filters) > 0 {
		filter = filters[0]
	}
	var tenant *uuid.UUID
	if !scope.Platform() {
		tenant = &scope.TenantID
	}
	// Attribute keys are sorted so the generated SQL is stable for identical
	// filters; values are passed as parameters, never inlined.
	keys := make([]string, 0, len(filter.Attributes))
	for key := range filter.Attributes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	args := []any{query, limit, tenant}
	var predicates []string
	for _, key := range keys {
		args = append(args, key, filter.Attributes[key])
		predicates = append(predicates, fmt.Sprintf("d.attributes @> jsonb_build_object($%d::text, $%d::jsonb)", len(args)-1, len(args)))
	}
	if !filter.ValidAt.IsZero() {
		args = append(args, filter.ValidAt)
		predicates = append(predicates, fmt.Sprintf("coalesce(d.valid_from, '-infinity'::timestamptz) <= $%d AND coalesce(d.valid_to, 'infinity'::timestamptz) >= $%d", len(args), len(args)))
	}
	clause := ""
	if len(predicates) > 0 {
		clause = " AND " + strings.Join(predicates, " AND ")
	}
	tx, err := (&ChatRepository{db: r.db}).begin(ctx, user, scope)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	query = strings.Join(strings.Fields(query), " ")
	rows, err := tx.QueryContext(ctx,
		`WITH q AS (
		     SELECT websearch_to_tsquery('indonesian'::regconfig, $1) AS query
		 ), scoped AS (
		     (SELECT c.id, c.position, c.document_id, d.title, d.filename, d.format, c.heading_path, c.locator, c.content,
		             d.attributes, d.valid_from, d.valid_to, d.version,
		             ts_rank_cd(c.search, q.query)::float8 AS score, true AS global
		        FROM knowledge_chunks c JOIN knowledge_documents d ON d.id = c.document_id CROSS JOIN q
		       WHERE c.search @@ q.query AND c.tenant_id IS NULL AND d.tenant_id IS NULL AND d.status='published'`+clause+`
		       ORDER BY score DESC, c.document_id, c.position, c.id LIMIT $2)
		     UNION ALL
		     (SELECT c.id, c.position, c.document_id, d.title, d.filename, d.format, c.heading_path, c.locator, c.content,
		             d.attributes, d.valid_from, d.valid_to, d.version,
		             ts_rank_cd(c.search, q.query)::float8 AS score, false AS global
		        FROM knowledge_chunks c JOIN knowledge_documents d ON d.id = c.document_id CROSS JOIN q
		       WHERE c.search @@ q.query AND c.tenant_id = $3::uuid AND d.tenant_id = $3::uuid AND d.status='published'`+clause+`
		       ORDER BY score DESC, c.document_id, c.position, c.id LIMIT $2)
		 ) SELECT * FROM scoped ORDER BY score DESC, document_id, position, id LIMIT $2`, args...)
	if err != nil {
		return nil, err
	}
	hits, err := collectRows(rows, func(row rowScanner) (models.KnowledgeHit, error) {
		var h models.KnowledgeHit
		var attributes []byte
		err := row.Scan(&h.ChunkID, &h.Position, &h.DocumentID, &h.Title, &h.Filename, &h.Format, &h.HeadingPath, &h.Locator, &h.Content, &attributes, &h.ValidFrom, &h.ValidTo, &h.Version, &h.Score, &h.Global)
		if err != nil {
			return h, err
		}
		digest := sha256.Sum256([]byte(h.Content))
		h.ContentHash = hex.EncodeToString(digest[:])
		h.Attributes = attributes
		return h, nil
	})
	if err != nil {
		return nil, err
	}
	return hits, tx.Commit()
}
