package repositories

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"hsg/internal/lib/apperr"
	"hsg/internal/models"
)

var ErrNoEmbeddingProfile = errors.New("embedding profile not configured")

// KnowledgeSemanticRepository reads indexed chunk embeddings. Config snapshots
// go through the owner pool because knowledge_embedding_config is admin-only;
// candidate search runs on the runtime pool with tenant RLS applied.
type KnowledgeSemanticRepository struct {
	db    *sql.DB
	owner *sql.DB
}

func NewKnowledgeSemanticRepository(runtime, owner *sql.DB) *KnowledgeSemanticRepository {
	return &KnowledgeSemanticRepository{db: runtime, owner: owner}
}

// Snapshot returns the active embedding configuration from the owner pool. The
// config is an admin-only singleton guarded by FORCE RLS, so it is read outside
// the tenant transaction: the owner connection bypasses RLS, and content reads
// stay on the runtime pool with tenant RLS applied. It fails closed when no
// profile is configured, so the hybrid path cannot silently run without a model.
func (r *KnowledgeSemanticRepository) Snapshot(ctx context.Context, user uuid.UUID, scope models.ChatScope) (models.KnowledgeEmbeddingConfig, error) {
	if r == nil || r.db == nil || r.owner == nil {
		return models.KnowledgeEmbeddingConfig{}, apperr.ErrForbidden
	}
	// Prove the caller may search this scope before touching the provider or
	// reading admin-only configuration.
	tx, err := (&ChatRepository{db: r.db}).begin(ctx, user, scope)
	if err != nil {
		return models.KnowledgeEmbeddingConfig{}, err
	}
	if err = tx.Rollback(); err != nil {
		return models.KnowledgeEmbeddingConfig{}, err
	}
	cfg, err := scanEmbeddingConfig(r.owner.QueryRowContext(ctx, embeddingConfigSelect))
	if errors.Is(err, sql.ErrNoRows) {
		return models.KnowledgeEmbeddingConfig{}, ErrNoEmbeddingProfile
	}
	if err != nil {
		return models.KnowledgeEmbeddingConfig{}, err
	}
	return cfg, nil
}

// encodeVector formats a normalized dense vector for pgvector, validating
// dimensions, finite values, and unit norm. The cosine operator handles
// similarity, so strict normalization is defensive against bad inputs.
func encodeVector(vector []float32) (string, error) {
	if len(vector) != models.KnowledgeEmbeddingDimensions {
		return "", errors.New("invalid embedding dimensions")
	}
	var norm float64
	var b strings.Builder
	b.WriteByte('[')
	for i, value := range vector {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return "", errors.New("non-finite embedding")
		}
		norm += float64(value) * float64(value)
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(value), 'g', -1, 32))
	}
	b.WriteByte(']')
	if math.Abs(norm-1) > 0.01 {
		return "", errors.New("embedding is not normalized")
	}
	return b.String(), nil
}

// Candidates returns lexical and semantic shortlists for the chat scope. When no
// profile id is configured both branches are empty and the caller falls back to
// lexical-only retrieval. Semantic uses exact cosine (<=>) with strict-order
// HNSW scan and a bounded statement timeout so a bad plan cannot stall chat.
func (r *KnowledgeSemanticRepository) Candidates(ctx context.Context, user uuid.UUID, scope models.ChatScope, query string, profile uuid.UUID, vector []float32, filters ...models.KnowledgeFilter) ([]models.KnowledgeHit, []models.KnowledgeHit, error) {
	if r == nil || r.db == nil {
		return nil, nil, apperr.ErrForbidden
	}
	encoded, err := encodeVector(vector)
	if err != nil {
		return nil, nil, err
	}
	tx, err := (&ChatRepository{db: r.db}).begin(ctx, user, scope)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SET LOCAL hnsw.iterative_scan='strict_order'`); err != nil {
		return nil, nil, err
	}
	if _, err = tx.ExecContext(ctx, `SET LOCAL hnsw.ef_search=100`); err != nil {
		return nil, nil, err
	}
	if _, err = tx.ExecContext(ctx, `SET LOCAL statement_timeout='1000ms'`); err != nil {
		return nil, nil, err
	}
	var tenant *uuid.UUID
	if !scope.Platform() {
		tenant = &scope.TenantID
	}
	var filter models.KnowledgeFilter
	if len(filters) > 0 {
		filter = filters[0]
	}
	// Attribute keys are sorted so the generated SQL is stable for identical
	// filters; values are passed as parameters, never inlined.
	keys := make([]string, 0, len(filter.Attributes))
	for key := range filter.Attributes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	args := []any{strings.Join(strings.Fields(query), " "), encoded, profile, tenant}
	var predicates []string
	for _, key := range keys {
		args = append(args, key, filter.Attributes[key])
		predicates = append(predicates, fmt.Sprintf("d.attributes @> jsonb_build_object($%d::text, $%d::jsonb)", len(args)-1, len(args)))
	}
	if !filter.ValidAt.IsZero() {
		// The window columns are DATE; compare as dates so the result does not
		// depend on the server timezone.
		args = append(args, filter.ValidAt.Format("2006-01-02"))
		predicates = append(predicates, fmt.Sprintf("coalesce(d.valid_from, '-infinity'::date) <= $%d::date AND coalesce(d.valid_to, 'infinity'::date) >= $%d::date", len(args), len(args)))
	}
	clause := ""
	if len(predicates) > 0 {
		clause = " AND " + strings.Join(predicates, " AND ")
	}
	rows, err := tx.QueryContext(ctx, hybridCandidateSQL(scope.Platform(), clause), args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	lexical, semantic := []models.KnowledgeHit{}, []models.KnowledgeHit{}
	for rows.Next() {
		var branch string
		var h models.KnowledgeHit
		var attributes []byte
		if err = rows.Scan(&branch, &h.ChunkID, &h.Position, &h.DocumentID, &h.Title, &h.Filename, &h.Format, &h.HeadingPath, &h.Locator, &h.Content, &h.Global, &h.ContentHash, &attributes, &h.ValidFrom, &h.ValidTo, &h.Version, &h.Score); err != nil {
			return nil, nil, err
		}
		h.Attributes = attributes
		if branch == "lexical" {
			lexical = append(lexical, h)
		} else {
			semantic = append(semantic, h)
		}
	}
	if err = rows.Err(); err != nil {
		return nil, nil, err
	}
	return lexical, semantic, tx.Commit()
}

// hybridCandidateSQL builds the unified candidate query. Each branch is scoped
// to global and tenant partitions via UNION ALL, mirroring the lexical path.
// Semantic matches by profile id and content hash so re-embedding under the same
// profile reuses vectors without drift.
func hybridCandidateSQL(tenant bool, clause string) string {
	lexical, semantic := []string{}, []string{}
	for _, local := range []bool{false, true} {
		if local && !tenant {
			continue
		}
		predicate := `c.tenant_id IS NULL AND d.tenant_id IS NULL AND $4::uuid IS NULL AND d.status='published'` + clause
		epredicate := `e.tenant_id IS NULL AND $4::uuid IS NULL`
		if local {
			predicate = `c.tenant_id=$4::uuid AND d.tenant_id=$4::uuid AND d.status='published'` + clause
			epredicate = `e.tenant_id=$4::uuid`
		}
		columns := `c.id,c.position,d.id AS document_id,d.title,d.filename,d.format,c.heading_path,c.locator,c.content,(d.tenant_id IS NULL) AS global,encode(sha256((c.heading_path||E'\n'||c.content)::bytea),'hex') AS content_hash,d.attributes,d.valid_from,d.valid_to,d.version`
		lexical = append(lexical, `(SELECT `+columns+`,ts_rank_cd(c.search,q.query)::float8 AS score FROM knowledge_chunks c JOIN knowledge_documents d ON d.id=c.document_id CROSS JOIN q WHERE `+predicate+` AND c.search @@ q.query ORDER BY score DESC,c.document_id,c.position,c.id LIMIT 50)`)
		semantic = append(semantic, `(SELECT `+columns+`,(e.embedding <=> $2::vector)::float8 AS score FROM knowledge_chunk_embeddings e JOIN knowledge_chunks c ON c.id=e.chunk_id AND c.tenant_id IS NOT DISTINCT FROM e.tenant_id JOIN knowledge_documents d ON d.id=c.document_id WHERE `+predicate+` AND `+epredicate+` AND e.profile_id=$3::uuid AND e.content_hash=encode(sha256((c.heading_path||E'\n'||c.content)::bytea),'hex') ORDER BY e.embedding <=> $2::vector LIMIT 50)`)
	}
	return `WITH q AS (SELECT websearch_to_tsquery('indonesian'::regconfig,$1) AS query), lex AS (` + strings.Join(lexical, ` UNION ALL `) + `), sem AS (` + strings.Join(semantic, ` UNION ALL `) + `) SELECT * FROM ((SELECT 'lexical' AS branch,* FROM lex ORDER BY score DESC,document_id,position,id LIMIT 50) UNION ALL (SELECT 'semantic' AS branch,* FROM sem ORDER BY score ASC,document_id,position,id LIMIT 50)) results`
}
