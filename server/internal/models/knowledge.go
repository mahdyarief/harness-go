package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// KnowledgeDocument is one uploaded reference file. TenantID nil means a global
// document uploaded by a super user; otherwise it belongs to that tenant only.
type KnowledgeDocument struct {
	ID                 uuid.UUID       `db:"id" json:"id"`
	TenantID           *uuid.UUID      `db:"tenant_id" json:"tenant_id"`
	CategoryID         *uuid.UUID      `db:"category_id" json:"category_id"`
	CategoryName       *string         `db:"category_name" json:"category_name"`
	Title              string          `db:"title" json:"title"`
	Filename           string          `db:"filename" json:"filename"`
	Format             string          `db:"format" json:"format"`
	SizeBytes          int64           `db:"size_bytes" json:"size_bytes"`
	CharCount          int             `db:"char_count" json:"char_count"`
	ChunkCount         int             `db:"chunk_count" json:"chunk_count"`
	ExtractionMetadata json.RawMessage `db:"extraction_metadata" json:"extraction_metadata"`
	Attributes         json.RawMessage `db:"attributes" json:"attributes"`
	ValidFrom          *time.Time      `db:"valid_from" json:"valid_from,omitempty"`
	ValidTo            *time.Time      `db:"valid_to" json:"valid_to,omitempty"`
	Version            string          `db:"version" json:"version,omitempty"`
	UploadedBy         *uuid.UUID      `db:"uploaded_by" json:"uploaded_by"`
	CreatedAt          time.Time       `db:"created_at" json:"created_at"`
	UpdatedAt          time.Time       `db:"updated_at" json:"updated_at"`
	Status             string          `db:"status" json:"status"`
}

// KnowledgeFilter narrows retrieval to documents whose attributes match every
// entry (JSON equality) and whose validity window covers ValidAt. Zero
// ValidAt means no date constraint.
type KnowledgeFilter struct {
	Attributes map[string]any
	ValidAt    time.Time
}

// Empty reports whether the filter constrains nothing, so the unfiltered SQL
// path stays byte-identical to the pre-filter query plan.
func (f KnowledgeFilter) Empty() bool {
	return len(f.Attributes) == 0 && f.ValidAt.IsZero()
}

// KnowledgeCategory groups documents inside one library. TenantID nil means the
// global category managed by a super_user; otherwise it belongs to that tenant.
type KnowledgeCategory struct {
	ID        uuid.UUID  `db:"id" json:"id"`
	TenantID  *uuid.UUID `db:"tenant_id" json:"tenant_id"`
	Name      string     `db:"name" json:"name"`
	Slug      string     `db:"slug" json:"slug"`
	CreatedAt time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt time.Time  `db:"updated_at" json:"updated_at"`
}

type KnowledgePreviewChunk struct {
	Position    int    `json:"position"`
	HeadingPath string `json:"heading_path"`
	Locator     string `json:"locator"`
	Content     string `json:"content"`
}

type KnowledgePreview struct {
	Document KnowledgeDocument       `json:"document"`
	Chunks   []KnowledgePreviewChunk `json:"chunks"`
}

// KnowledgeHit is one passage returned by search, with the provenance the
// model needs to cite it.
type KnowledgeHit struct {
	ChunkID     uuid.UUID
	Position    int
	ContentHash string
	DocumentID  uuid.UUID
	Title       string
	Filename    string
	Format      string
	HeadingPath string
	Locator     string
	Content     string
	Score       float64
	Global      bool
	// Metadata travels to evidence so a citation can name the regulation
	// version and its attributes without a second query.
	Attributes json.RawMessage
	ValidFrom  *time.Time
	ValidTo    *time.Time
	Version    string
	// Source records which retrieval branch produced this hit: "lexical",
	// "semantic", or "both". Lexical-only search leaves it empty.
	Source string
}
