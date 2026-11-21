package repositories

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/google/uuid"
	"hsg/internal/lib/apperr"
	"hsg/internal/models"
)

type KnowledgeEmbeddingJob struct {
	ID         uuid.UUID
	DocumentID uuid.UUID
	ProfileID  uuid.UUID
	Fence      int64
}
type KnowledgeEmbeddingInput struct {
	ChunkID     uuid.UUID
	Position    int
	ContentHash string
	Input       string
}
type KnowledgeEmbeddingOutput struct {
	ChunkID     uuid.UUID `json:"chunk_id"`
	ContentHash string    `json:"content_hash"`
	Embedding   string    `json:"embedding"`
}

type KnowledgeEmbeddingWorkerRepository struct{ db *sql.DB }

func NewKnowledgeEmbeddingWorkerRepository(db *sql.DB) *KnowledgeEmbeddingWorkerRepository {
	return &KnowledgeEmbeddingWorkerRepository{db: db}
}

func (r *KnowledgeEmbeddingWorkerRepository) Claim(ctx context.Context) (KnowledgeEmbeddingJob, error) {
	var j KnowledgeEmbeddingJob
	err := r.db.QueryRowContext(ctx, `SELECT job_id,document_id,profile_id,fence_token FROM knowledge_embedding_claim()`).Scan(&j.ID, &j.DocumentID, &j.ProfileID, &j.Fence)
	return j, translateNotFound("embedding job", err)
}
func (r *KnowledgeEmbeddingWorkerRepository) Config(ctx context.Context, j KnowledgeEmbeddingJob) (models.KnowledgeEmbeddingConfig, error) {
	return scanEmbeddingConfig(r.db.QueryRowContext(ctx, `SELECT * FROM knowledge_embedding_worker_config($1,$2)`, j.ID, j.Fence))
}
func (r *KnowledgeEmbeddingWorkerRepository) Read(ctx context.Context, j KnowledgeEmbeddingJob) ([]KnowledgeEmbeddingInput, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT chunk_id,position,content_hash,input FROM knowledge_embedding_read($1,$2)`, j.ID, j.Fence)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]KnowledgeEmbeddingInput, 0, 32)
	for rows.Next() {
		var v KnowledgeEmbeddingInput
		if err = rows.Scan(&v.ChunkID, &v.Position, &v.ContentHash, &v.Input); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *KnowledgeEmbeddingWorkerRepository) Commit(ctx context.Context, j KnowledgeEmbeddingJob, items []KnowledgeEmbeddingOutput) (bool, error) {
	if items == nil {
		items = []KnowledgeEmbeddingOutput{}
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return false, err
	}
	var ok bool
	err = r.db.QueryRowContext(ctx, `SELECT knowledge_embedding_commit($1,$2,$3::jsonb)`, j.ID, j.Fence, string(raw)).Scan(&ok)
	return ok, err
}
func (r *KnowledgeEmbeddingWorkerRepository) Fail(ctx context.Context, j KnowledgeEmbeddingJob, invalid bool) error {
	code := "provider_unavailable"
	if invalid {
		code = "provider_invalid"
	}
	_, err := r.db.ExecContext(ctx, `SELECT knowledge_embedding_fail($1,$2,$3)`, j.ID, j.Fence, code)
	return err
}
func (r *KnowledgeEmbeddingWorkerRepository) ValidateRole(ctx context.Context) error {
	var unsafe bool
	err := r.db.QueryRowContext(ctx, `SELECT rolsuper OR rolbypassrls OR rolcreaterole OR rolcreatedb OR rolreplication
 OR NOT pg_has_role(current_user,'hsg_embedding_worker','USAGE')
 OR pg_has_role(current_user,'hsg_embedding_executor','MEMBER')
 OR EXISTS(SELECT 1 FROM pg_roles r WHERE r.rolname<>current_user AND pg_has_role(current_user,r.oid,'MEMBER') AND (r.rolsuper OR r.rolbypassrls OR r.rolcreaterole OR r.rolcreatedb))
 OR EXISTS(SELECT 1 FROM pg_class WHERE relnamespace='public'::regnamespace AND relkind IN ('r','p') AND pg_has_role(current_user,relowner,'MEMBER'))
 OR EXISTS(SELECT 1 FROM pg_class WHERE relnamespace='public'::regnamespace AND relkind IN ('r','p') AND (has_table_privilege(current_user,oid,'SELECT') OR has_table_privilege(current_user,oid,'INSERT') OR has_table_privilege(current_user,oid,'UPDATE') OR has_table_privilege(current_user,oid,'DELETE')))
 FROM pg_roles WHERE rolname=current_user`).Scan(&unsafe)
	if err != nil {
		return err
	}
	if unsafe {
		return apperr.New(apperr.KindForbidden, "unsafe embedding worker database role")
	}
	return nil
}
