package repositories

import (
	"context"
	"database/sql"
)

// KnowledgeCleanupRepository uses the owner pool, never the tenant runtime
// pool. The queue deliberately has no runtime grants or HTTP surface.
type KnowledgeCleanupRepository struct{ db *sql.DB }

func NewKnowledgeCleanupRepository(owner *sql.DB) *KnowledgeCleanupRepository {
	return &KnowledgeCleanupRepository{db: owner}
}

func (r *KnowledgeCleanupRepository) Pending(ctx context.Context, limit int) ([]string, error) {
	if limit < 1 || limit > 100 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `SELECT storage_key FROM public.knowledge_blob_cleanup ORDER BY queued_at, storage_key LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := make([]string, 0)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

// Complete is called only after an idempotent storage delete succeeds. A
// failed/unknown acknowledgement leaves a harmless retry of the same key.
func (r *KnowledgeCleanupRepository) Complete(ctx context.Context, key string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM public.knowledge_blob_cleanup WHERE storage_key=$1`, key)
	return err
}
