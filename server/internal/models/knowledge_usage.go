package models

import (
	"time"

	"github.com/google/uuid"
)

// KnowledgeUsageEvent is one append-only row of the usage log. A 'retrieved'
// event means the passage was offered to the model; 'cited' means the final
// answer surfaced the document as a reference. Source records which retrieval
// branch produced it so embedding contribution can be measured.
type KnowledgeUsageEvent struct {
	DocumentID uuid.UUID
	ChunkID    uuid.UUID
	SessionID  uuid.UUID
	Event      string
	Source     string
}

// KnowledgeDocumentUsage is the aggregated usage of one document in a window.
type KnowledgeDocumentUsage struct {
	Document           KnowledgeDocument `json:"document"`
	Citations          int               `json:"citations"`
	Retrievals         int               `json:"retrievals"`
	SemanticCitations  int               `json:"semantic_citations"`
	SemanticRetrievals int               `json:"semantic_retrievals"`
	LastUsedAt         time.Time         `json:"last_used_at"`
}

// CitationRate is the share of retrievals that ended in a citation.
func (u KnowledgeDocumentUsage) CitationRate() float64 {
	if u.Retrievals == 0 {
		return 0
	}
	return float64(u.Citations) / float64(u.Retrievals)
}

// SemanticShare is the share of retrievals that came from the semantic branch.
// It answers directly whether the embedding index is contributing anything on
// top of lexical search.
func (u KnowledgeDocumentUsage) SemanticShare() float64 {
	if u.Retrievals == 0 {
		return 0
	}
	return float64(u.SemanticRetrievals) / float64(u.Retrievals)
}
