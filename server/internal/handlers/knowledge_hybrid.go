package handlers

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"errors"

	"github.com/google/uuid"
	"hsg/internal/dtos"
	"hsg/internal/lib/kb"
	"hsg/internal/lib/kbembed"
	"hsg/internal/models"
	"hsg/internal/repositories"
)

// hybridSearcher augments lexical retrieval with semantic candidates when an
// embedding profile is active. It embeds the query with the pinned model, merges
// the two ranked branches with weighted reciprocal rank fusion, then applies the
// same diversity-aware selection as lexical-only retrieval. When no profile is
// configured it returns nil so the caller stays on the lexical path.
type hybridSearcher struct {
	repo         *repositories.KnowledgeSemanticRepository
	key          []byte
	allowedHosts []string
	allowPrivate bool
}

func (s *hybridSearcher) Search(ctx context.Context, user uuid.UUID, scope models.ChatScope, query string, limit int, filters ...models.KnowledgeFilter) ([]models.KnowledgeHit, error) {
	if s == nil || s.repo == nil {
		return nil, nil
	}
	cfg, err := s.repo.Snapshot(ctx, user, scope)
	if err != nil {
		if errors.Is(err, repositories.ErrNoEmbeddingProfile) {
			return nil, nil
		}
		return nil, err
	}
	if !cfg.Enabled || cfg.ProfileID == "" || cfg.Revision == "" {
		return nil, nil
	}
	profileID, err := uuid.Parse(cfg.ProfileID)
	if err != nil {
		return nil, err
	}
	credential, err := decryptEmbeddingCredential(s.key, cfg)
	if err != nil {
		return nil, err
	}
	client, err := kbembed.New(kbembed.Config{
		Endpoint:      cfg.Endpoint,
		Model:         cfg.Model,
		Revision:      cfg.Revision,
		Dimensions:    cfg.Dimensions,
		Credential:    credential,
		AllowedHosts:  s.allowedHosts,
		AllowPrivate:  s.allowPrivate,
		MaxBatchSize:  1,
		MaxInputBytes: 0,
	})
	if err != nil {
		return nil, err
	}
	vectors, err := client.Embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	if len(vectors) != 1 {
		return nil, errors.New("embedding service returned no vector")
	}
	var filter models.KnowledgeFilter
	if len(filters) > 0 {
		filter = filters[0]
	}
	lexical, semantic, err := s.repo.Candidates(ctx, user, scope, query, profileID, vectors[0], filter)
	if err != nil {
		return nil, err
	}
	ranked := kb.WeightedRRF(toCandidates(lexical), toCandidates(semantic), 1, 1)
	selected := fromCandidates(kb.SelectCandidates(ranked, limit))
	markHitSources(selected, lexical, semantic)
	return selected, nil
}

// markHitSources tags each selected hit with the branch(es) that produced it,
// so usage logging can separate embedding contribution from lexical match.
func markHitSources(hits, lexical, semantic []models.KnowledgeHit) {
	lex := make(map[uuid.UUID]struct{}, len(lexical))
	for _, h := range lexical {
		lex[h.ChunkID] = struct{}{}
	}
	sem := make(map[uuid.UUID]struct{}, len(semantic))
	for _, h := range semantic {
		sem[h.ChunkID] = struct{}{}
	}
	for i := range hits {
		_, inLex := lex[hits[i].ChunkID]
		_, inSem := sem[hits[i].ChunkID]
		switch {
		case inLex && inSem:
			hits[i].Source = "both"
		case inSem:
			hits[i].Source = "semantic"
		default:
			hits[i].Source = "lexical"
		}
	}
}

// decryptEmbeddingCredential reverses embeddingCredential without re-sealing.
func decryptEmbeddingCredential(key []byte, c models.KnowledgeEmbeddingConfig) (string, error) {
	if len(key) != 32 {
		return "", errors.New("embedding encryption key unavailable")
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	g, err := cipher.NewGCM(b)
	if err != nil {
		return "", err
	}
	aad := []byte("hsg/knowledge-embedding/credential/v1/config/singleton|" + c.Endpoint + "|" + dtos.KnowledgeEmbeddingFingerprint(c.Revision))
	if len(c.EncryptedCredential) < 1+g.NonceSize()+g.Overhead() || c.EncryptedCredential[0] != 1 {
		return "", errors.New("invalid embedding credential")
	}
	plain, err := g.Open(nil, c.EncryptedCredential[1:1+g.NonceSize()], c.EncryptedCredential[1+g.NonceSize():], aad)
	if err != nil {
		return "", errors.New("invalid embedding credential")
	}
	return string(plain), nil
}

func toCandidates(hits []models.KnowledgeHit) []kb.Candidate {
	out := make([]kb.Candidate, len(hits))
	for i, h := range hits {
		out[i] = kb.Candidate{
			ChunkID:     h.ChunkID.String(),
			DocumentID:  h.DocumentID.String(),
			Position:    h.Position,
			Title:       h.Title,
			HeadingPath: h.HeadingPath,
			Locator:     h.Locator,
			Filename:    h.Filename,
			Format:      h.Format,
			Content:     h.Content,
			ContentHash: h.ContentHash,
			Attributes:  h.Attributes,
			ValidFrom:   h.ValidFrom,
			ValidTo:     h.ValidTo,
			Version:     h.Version,
			Global:      h.Global,
		}
	}
	return out
}

func fromCandidates(candidates []kb.Candidate) []models.KnowledgeHit {
	out := make([]models.KnowledgeHit, len(candidates))
	for i, c := range candidates {
		hit := models.KnowledgeHit{
			Position:    c.Position,
			Title:       c.Title,
			Filename:    c.Filename,
			Format:      c.Format,
			HeadingPath: c.HeadingPath,
			Locator:     c.Locator,
			Content:     c.Content,
			ContentHash: c.ContentHash,
			Attributes:  c.Attributes,
			ValidFrom:   c.ValidFrom,
			ValidTo:     c.ValidTo,
			Version:     c.Version,
			Global:      c.Global,
		}
		if id, err := uuid.Parse(c.ChunkID); err == nil {
			hit.ChunkID = id
		}
		if id, err := uuid.Parse(c.DocumentID); err == nil {
			hit.DocumentID = id
		}
		out[i] = hit
	}
	return out
}
