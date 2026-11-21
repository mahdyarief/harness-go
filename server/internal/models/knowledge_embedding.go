package models

import "time"

const KnowledgeEmbeddingModel = "BAAI/bge-m3"
const KnowledgeEmbeddingDimensions = 1024

type KnowledgeEmbeddingConfig struct {
	Endpoint            string    `json:"endpoint"`
	Model               string    `json:"model"`
	Revision            string    `json:"revision"`
	Dimensions          int       `json:"dimensions"`
	Enabled             bool      `json:"enabled"`
	HasCredential       bool      `json:"has_credential"`
	UpdatedAt           time.Time `json:"updated_at"`
	ProfileID           string    `json:"-"`
	Generation          int64     `json:"-"`
	EncryptedCredential []byte    `json:"-"`
}

type KnowledgeIndexing struct {
	Status          string    `json:"status"`
	IndexedChunks   int       `json:"indexed_chunks"`
	EligibleChunks  int       `json:"eligible_chunks"`
	ProfileRevision string    `json:"profile_revision"`
	UpdatedAt       time.Time `json:"updated_at"`
	ErrorCode       string    `json:"error_code,omitempty"`
}
