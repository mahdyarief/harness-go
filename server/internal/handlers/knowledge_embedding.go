package handlers

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"hsg/internal/dtos"
	"hsg/internal/lib/apperr"
	"hsg/internal/models"
	"hsg/internal/repositories"
)

// The adapter must validate operator-allowed endpoints, the model revision and dense output.
type KnowledgeEmbeddingProbe func(context.Context, models.KnowledgeEmbeddingConfig, string) error

type KnowledgeEmbeddingHandler struct {
	repo  *repositories.KnowledgeEmbeddingRepository
	key   []byte
	probe KnowledgeEmbeddingProbe
}

func NewKnowledgeEmbeddingHandler(repo *repositories.KnowledgeEmbeddingRepository, key []byte, probe KnowledgeEmbeddingProbe) *KnowledgeEmbeddingHandler {
	return &KnowledgeEmbeddingHandler{repo: repo, key: key, probe: probe}
}

// AAD separates embedding credentials from chat secrets and binds them to endpoint/profile.
func embeddingCredential(key []byte, c models.KnowledgeEmbeddingConfig, input []byte, seal bool) ([]byte, error) {
	if len(key) != 32 {
		return nil, errors.New("embedding encryption key unavailable")
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(b)
	if err != nil {
		return nil, err
	}
	aad := []byte("hsg/knowledge-embedding/credential/v1/config/singleton|" + c.Endpoint + "|" + dtos.KnowledgeEmbeddingFingerprint(c.Revision))
	if seal {
		n := make([]byte, g.NonceSize())
		if _, err = rand.Read(n); err != nil {
			return nil, err
		}
		return g.Seal(append([]byte{1}, n...), n, input, aad), nil
	}
	if len(input) < 1+g.NonceSize()+g.Overhead() || input[0] != 1 {
		return nil, errors.New("invalid embedding credential")
	}
	return g.Open(nil, input[1:1+g.NonceSize()], input[1+g.NonceSize():], aad)
}

func (h *KnowledgeEmbeddingHandler) Get(c fiber.Ctx) error {
	actor, _, err := knowledgeScope(c)
	if err != nil {
		return err
	}
	cfg, err := h.repo.Get(c.Context(), actor)
	if err != nil {
		return err
	}
	return dtos.OK(c, cfg)
}

func (h *KnowledgeEmbeddingHandler) Update(c fiber.Ctx) error {
	actor, _, err := knowledgeScope(c)
	if err != nil {
		return err
	}
	var body dtos.KnowledgeEmbeddingUpdate
	if err = strictLLMBody(c, &body); err != nil {
		return err
	}
	if err = body.Validate(); err != nil {
		return err
	}
	old, err := h.repo.Get(c.Context(), actor)
	if err != nil {
		return err
	}
	cfg := models.KnowledgeEmbeddingConfig{Endpoint: body.Endpoint, Model: body.Model, Revision: body.Revision, Dimensions: body.Dimensions, Enabled: body.Enabled}
	var credential string
	if body.Credential != nil {
		credential = *body.Credential
	} else if !body.ClearCredential && old.HasCredential {
		if old.Endpoint != cfg.Endpoint || old.Revision != cfg.Revision {
			return apperr.New(apperr.KindBadRequest, "Perubahan endpoint/profil membutuhkan credential baru atau penghapusan credential")
		}
		plaintext, e := embeddingCredential(h.key, old, old.EncryptedCredential, false)
		if e != nil {
			return fiber.NewError(503, "Credential embedding tidak tersedia")
		}
		credential = string(plaintext)
	}
	if credential != "" {
		cfg.EncryptedCredential, err = embeddingCredential(h.key, cfg, []byte(credential), true)
		if err != nil {
			return fiber.NewError(503, "Kunci embedding tidak tersedia")
		}
	}
	if cfg.Enabled {
		if h.probe == nil {
			return fiber.NewError(503, "Adapter embedding belum tersedia")
		}
		if err = h.probe(c.Context(), cfg, credential); err != nil {
			return fiber.NewError(422, "Pemeriksaan kesehatan embedding gagal")
		}
	}
	if err = h.repo.Save(c.Context(), actor, cfg, old.Generation); err != nil {
		return err
	}
	return h.Get(c)
}

func (h *KnowledgeEmbeddingHandler) Test(c fiber.Ctx) error {
	actor, _, err := knowledgeScope(c)
	if err != nil {
		return err
	}
	cfg, err := h.repo.Get(c.Context(), actor)
	if err != nil {
		return err
	}
	if h.probe == nil {
		return fiber.NewError(503, "Adapter embedding belum tersedia")
	}
	var credential []byte
	if cfg.HasCredential {
		credential, err = embeddingCredential(h.key, cfg, cfg.EncryptedCredential, false)
		if err != nil {
			return fiber.NewError(503, "Credential embedding tidak tersedia")
		}
	}
	if err = h.probe(c.Context(), cfg, string(credential)); err != nil {
		return fiber.NewError(422, "Pemeriksaan kesehatan embedding gagal")
	}
	return dtos.OK(c, fiber.Map{"ok": true, "message": "Embedding siap", "model": cfg.Model, "revision": cfg.Revision, "dimensions": cfg.Dimensions})
}

func (h *KnowledgeEmbeddingHandler) Indexing(c fiber.Ctx) error { return h.indexing(c, false) }
func (h *KnowledgeEmbeddingHandler) Retry(c fiber.Ctx) error    { return h.indexing(c, true) }
func (h *KnowledgeEmbeddingHandler) indexing(c fiber.Ctx, retry bool) error {
	actor, tenant, err := knowledgeScope(c)
	if err != nil {
		return err
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil || id == uuid.Nil {
		return fiber.NewError(400, "Dokumen tidak valid")
	}
	out, err := h.repo.Status(c.Context(), actor, tenant, id, retry)
	if err != nil {
		return err
	}
	if retry {
		doc, err := h.repo.Document(c.Context(), actor, tenant, id)
		if err != nil {
			return err
		}
		return dtos.OK(c, struct {
			models.KnowledgeDocument
			Indexing models.KnowledgeIndexing `json:"indexing"`
		}{doc, out})
	}
	return dtos.OK(c, out)
}
