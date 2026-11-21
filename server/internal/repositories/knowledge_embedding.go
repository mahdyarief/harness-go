package repositories

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"hsg/internal/lib/apperr"
	"hsg/internal/models"
)

type KnowledgeEmbeddingRepository struct{ db *sql.DB }

func NewKnowledgeEmbeddingRepository(db *sql.DB) *KnowledgeEmbeddingRepository {
	return &KnowledgeEmbeddingRepository{db: db}
}

const embeddingConfigSelect = `SELECT c.endpoint,coalesce(p.model,'BAAI/bge-m3'),coalesce(p.revision,''),coalesce(p.dimensions,1024),c.enabled,c.encrypted_credential,c.updated_at,coalesce(c.profile_id::text,''),c.generation FROM knowledge_embedding_config c LEFT JOIN knowledge_embedding_profiles p ON p.id=c.profile_id WHERE c.singleton`

func scanEmbeddingConfig(row *sql.Row) (models.KnowledgeEmbeddingConfig, error) {
	var c models.KnowledgeEmbeddingConfig
	err := row.Scan(&c.Endpoint, &c.Model, &c.Revision, &c.Dimensions, &c.Enabled, &c.EncryptedCredential, &c.UpdatedAt, &c.ProfileID, &c.Generation)
	c.HasCredential = len(c.EncryptedCredential) > 0
	return c, err
}

func (r *KnowledgeEmbeddingRepository) Get(ctx context.Context, actor uuid.UUID) (models.KnowledgeEmbeddingConfig, error) {
	tx, _, err := NewKnowledgeRepository(r.db).begin(ctx, actor, nil, true)
	if err != nil {
		return models.KnowledgeEmbeddingConfig{}, err
	}
	defer tx.Rollback()
	c, err := scanEmbeddingConfig(tx.QueryRowContext(ctx, embeddingConfigSelect))
	if err != nil {
		return c, err
	}
	return c, tx.Commit()
}

// Save uses optimistic concurrency: a health probe cannot enable a newer configuration.
func (r *KnowledgeEmbeddingRepository) Save(ctx context.Context, actor uuid.UUID, c models.KnowledgeEmbeddingConfig, expected int64) error {
	tx, _, err := NewKnowledgeRepository(r.db).begin(ctx, actor, nil, true)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var profile uuid.UUID
	err = tx.QueryRowContext(ctx, `INSERT INTO knowledge_embedding_profiles(revision) VALUES($1) ON CONFLICT DO NOTHING RETURNING id`, c.Revision).Scan(&profile)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `SELECT id FROM knowledge_embedding_profiles WHERE revision=$1`, c.Revision).Scan(&profile)
	}
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE knowledge_embedding_config SET endpoint=$1,profile_id=$2,enabled=$3,encrypted_credential=$4,generation=generation+1,backfill_after=NULL,backfill_done=false,updated_at=now() WHERE singleton AND generation=$5`, c.Endpoint, profile, c.Enabled, c.EncryptedCredential, expected)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return apperr.New(apperr.KindConflict, "Konfigurasi berubah; ulangi permintaan")
	}
	return tx.Commit()
}

func (r *KnowledgeEmbeddingRepository) Status(ctx context.Context, actor uuid.UUID, tenant *uuid.UUID, id uuid.UUID, retry bool) (models.KnowledgeIndexing, error) {
	var out models.KnowledgeIndexing
	tx, _, err := NewKnowledgeRepository(r.db).begin(ctx, actor, tenant, retry)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	var chunks int
	err = tx.QueryRowContext(ctx, `SELECT chunk_count FROM knowledge_documents WHERE id=$1 AND tenant_id IS NOT DISTINCT FROM $2::uuid`, id, tenant).Scan(&chunks)
	if errors.Is(err, sql.ErrNoRows) {
		return out, apperr.ErrNotFound
	}
	if err != nil {
		return out, err
	}
	if retry {
		_, err = tx.ExecContext(ctx, `SELECT knowledge_embedding_retry($1,$2::uuid)`, id, tenant)
		if err != nil {
			return out, err
		}
	}
	err = tx.QueryRowContext(ctx, `SELECT status,indexed_chunks,profile_revision,updated_at,error_code FROM knowledge_embedding_status($1,$2::uuid)`, id, tenant).Scan(&out.Status, &out.IndexedChunks, &out.ProfileRevision, &out.UpdatedAt, &out.ErrorCode)
	out.EligibleChunks = chunks
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}

func (r *KnowledgeEmbeddingRepository) Document(ctx context.Context, actor uuid.UUID, tenant *uuid.UUID, id uuid.UUID) (models.KnowledgeDocument, error) {
	p, err := NewKnowledgeRepository(r.db).Preview(ctx, actor, tenant, id, 0, 1)
	return p.Document, err
}

// ValidateRuntimeRole extends the shared tenant guard for this migration's tables.
func (r *KnowledgeEmbeddingRepository) ValidateRuntimeRole(ctx context.Context) error {
	unsafe, err := TenantRoleUnsafe(ctx, r.db)
	if err != nil {
		return err
	}
	if unsafe {
		return apperr.New(apperr.KindForbidden, "unsafe embedding runtime database role")
	}
	err = r.db.QueryRowContext(ctx, `SELECT
 (SELECT count(*) FROM pg_class WHERE relnamespace='public'::regnamespace AND relname IN ('knowledge_embedding_profiles','knowledge_embedding_config','knowledge_embedding_jobs','knowledge_chunk_embeddings') AND relrowsecurity AND relforcerowsecurity)<>4
 OR pg_has_role(current_user,'hsg_embedding_executor','MEMBER')
 OR pg_has_role(current_user,'hsg_embedding_worker','MEMBER')`).Scan(&unsafe)
	if err != nil {
		return err
	}
	if unsafe {
		return apperr.New(apperr.KindForbidden, "embedding runtime requires forced RLS and no worker membership")
	}
	return nil
}
