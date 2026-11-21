package repositories

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"hsg/internal/lib/apperr"
	"hsg/internal/models"
)

// KnowledgeUsageRepository reads and appends the knowledge usage log. Every
// operation runs inside a scoped transaction (KnowledgeRepository.begin) so the
// RLS policies on knowledge_document_usage_log evaluate against the caller's
// proven identity instead of an empty session context.
type KnowledgeUsageRepository struct{ db *sql.DB }

func NewKnowledgeUsageRepository(db *sql.DB) *KnowledgeUsageRepository {
	return &KnowledgeUsageRepository{db: db}
}

// LogEvents appends usage rows. The log is append-only and best-effort: the
// caller invokes it after the answer is already produced, so a failure must not
// surface to the user.
func (r *KnowledgeUsageRepository) LogEvents(ctx context.Context, actor uuid.UUID, tenant *uuid.UUID, events []models.KnowledgeUsageEvent) error {
	if r == nil || r.db == nil {
		return apperr.ErrForbidden
	}
	if len(events) == 0 {
		return nil
	}
	tx, _, err := (&KnowledgeRepository{db: r.db}).begin(ctx, actor, tenant, true)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var b strings.Builder
	b.WriteString(`INSERT INTO knowledge_document_usage_log (tenant_id, document_id, chunk_id, event, source, session_id) VALUES `)
	args := make([]any, 0, len(events)*6)
	for i, e := range events {
		if i > 0 {
			b.WriteString(",")
		}
		offset := i * 6
		fmt.Fprintf(&b, "($%d,$%d,$%d,$%d,$%d,$%d)", offset+1, offset+2, offset+3, offset+4, offset+5, offset+6)
		var chunkID any
		if e.ChunkID != uuid.Nil {
			chunkID = e.ChunkID
		}
		var sessionID any
		if e.SessionID != uuid.Nil {
			sessionID = e.SessionID
		}
		args = append(args, tenant, e.DocumentID, chunkID, e.Event, e.Source, sessionID)
	}
	if _, err = tx.ExecContext(ctx, b.String(), args...); err != nil {
		return fmt.Errorf("gagal menulis log penggunaan: %w", err)
	}
	return tx.Commit()
}

// DocumentUsage aggregates one document's usage inside the caller's scope.
func (r *KnowledgeUsageRepository) DocumentUsage(ctx context.Context, actor uuid.UUID, tenant *uuid.UUID, documentID uuid.UUID, from, to time.Time) (models.KnowledgeDocumentUsage, error) {
	if r == nil || r.db == nil {
		return models.KnowledgeDocumentUsage{}, apperr.ErrForbidden
	}
	tx, _, err := (&KnowledgeRepository{db: r.db}).begin(ctx, actor, tenant, false)
	if err != nil {
		return models.KnowledgeDocumentUsage{}, err
	}
	defer tx.Rollback()

	var out models.KnowledgeDocumentUsage
	if err = tx.QueryRowContext(ctx, `SELECT id, tenant_id, title, filename, format, char_count, chunk_count FROM knowledge_documents WHERE id = $1`, documentID).Scan(
		&out.Document.ID, &out.Document.TenantID, &out.Document.Title, &out.Document.Filename, &out.Document.Format, &out.Document.CharCount, &out.Document.ChunkCount,
	); err != nil {
		if err == sql.ErrNoRows {
			return models.KnowledgeDocumentUsage{}, apperr.New(apperr.KindNotFound, "Dokumen tidak ditemukan")
		}
		return models.KnowledgeDocumentUsage{}, err
	}
	if err = scanUsageAggregate(tx.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN event = 'cited' THEN n ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN event = 'retrieved' THEN n ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN source = 'semantic' AND event = 'cited' THEN n ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN source = 'semantic' THEN n ELSE 0 END), 0),
			COALESCE(MAX(last_at), $3)
		FROM (
			SELECT event, source, COUNT(*) AS n, MAX(created_at) AS last_at
			FROM knowledge_document_usage_log
			WHERE document_id = $1 AND created_at >= $2 AND created_at < $3
			GROUP BY event, source
		) windowed`, documentID, from, to), &out); err != nil && err != sql.ErrNoRows {
		return models.KnowledgeDocumentUsage{}, fmt.Errorf("gagal menghitung metrik: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return models.KnowledgeDocumentUsage{}, err
	}
	return out, nil
}

// DocumentUsageWindow aggregates every document with activity in the window.
// Documents with zero events are intentionally absent: the caller merges this
// list into the full document list so untouched documents read as zero.
func (r *KnowledgeUsageRepository) DocumentUsageWindow(ctx context.Context, actor uuid.UUID, tenant *uuid.UUID, from, to time.Time) ([]models.KnowledgeDocumentUsage, error) {
	if r == nil || r.db == nil {
		return nil, apperr.ErrForbidden
	}
	tx, _, err := (&KnowledgeRepository{db: r.db}).begin(ctx, actor, tenant, false)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		WITH windowed AS (
			SELECT document_id, event, source, COUNT(*) AS n, MAX(created_at) AS last_at
			FROM knowledge_document_usage_log
			WHERE ($1::uuid IS NULL OR tenant_id = $1) AND ($1::uuid IS NOT NULL OR tenant_id IS NULL)
			  AND created_at >= $2 AND created_at < $3
			GROUP BY document_id, event, source
		)
		SELECT w.document_id, d.title, d.filename, d.format, d.tenant_id,
			COALESCE(SUM(CASE WHEN w.event = 'cited' THEN w.n ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN w.event = 'retrieved' THEN w.n ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN w.source = 'semantic' AND w.event = 'cited' THEN w.n ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN w.source = 'semantic' THEN w.n ELSE 0 END), 0),
			COALESCE(MAX(w.last_at), $2)
		FROM windowed w
		JOIN knowledge_documents d ON d.id = w.document_id
		GROUP BY w.document_id, d.title, d.filename, d.format, d.tenant_id
		ORDER BY 6 DESC, 7 DESC, d.title`, tenant, from, to)
	if err != nil {
		return nil, fmt.Errorf("gagal menghitung metrik: %w", err)
	}
	defer rows.Close()

	out := make([]models.KnowledgeDocumentUsage, 0, 48)
	for rows.Next() {
		var u models.KnowledgeDocumentUsage
		if err = rows.Scan(&u.Document.ID, &u.Document.Title, &u.Document.Filename, &u.Document.Format, &u.Document.TenantID,
			&u.Citations, &u.Retrievals, &u.SemanticCitations, &u.SemanticRetrievals, &u.LastUsedAt); err != nil {
			return nil, fmt.Errorf("gagal membaca metrik: %w", err)
		}
		out = append(out, u)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("gagal membaca metrik: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

type usageRowScanner interface {
	Scan(dest ...any) error
}

func scanUsageAggregate(row usageRowScanner, u *models.KnowledgeDocumentUsage) error {
	return row.Scan(&u.Citations, &u.Retrievals, &u.SemanticCitations, &u.SemanticRetrievals, &u.LastUsedAt)
}
