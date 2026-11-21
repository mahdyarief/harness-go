package kb

import "context"

// Hit is one retrieved passage with enough provenance for the model to cite it.
type Hit struct {
	DocumentID  string
	Title       string
	HeadingPath string
	Locator     string
	Content     string
	Score       float64
	// Global is true for documents uploaded by the platform, false for the
	// caller's own tenant documents.
	Global bool
}

// Retriever finds passages relevant to a query. The caller's identity and tenant
// come from the request context through the repository, never from the model,
// so a tool cannot widen its own reach. Postgres full-text search is the first
// implementation; an embedding-based one can replace it without changing the
// agent tool.
type Retriever interface {
	Search(ctx context.Context, query string, limit int) ([]Hit, error)
}
