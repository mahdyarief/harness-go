package handlers

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"hsg/internal/dtos"
	"hsg/internal/lib"
	"hsg/internal/lib/apperr"
	"hsg/internal/repositories"
)

const (
	maxMetadataAttributes = 10
	maxMetadataKeyLen     = 100
	maxMetadataVersionLen = 64
)

// knowledgeMetadata is the parsed retrieval metadata accepted at upload time.
// A nil Attributes or Version leaves the column default; nil dates keep the
// validity window open.
type knowledgeMetadata struct {
	Attributes json.RawMessage
	ValidFrom  *time.Time
	ValidTo    *time.Time
	Version    string
}

func parseKnowledgeMetadataForm(get func(string) string) (knowledgeMetadata, error) {
	attributes, err := parseAttributesJSON(get("attributes"))
	if err != nil {
		return knowledgeMetadata{}, err
	}
	from, err := parseOptionalDate(get("valid_from"), "valid_from")
	if err != nil {
		return knowledgeMetadata{}, err
	}
	to, err := parseOptionalDate(get("valid_to"), "valid_to")
	if err != nil {
		return knowledgeMetadata{}, err
	}
	if err = checkWindow(from, to); err != nil {
		return knowledgeMetadata{}, err
	}
	version := strings.TrimSpace(get("version"))
	if len([]rune(version)) > maxMetadataVersionLen {
		return knowledgeMetadata{}, apperr.New(apperr.KindBadRequest, "version maksimum 64 karakter")
	}
	return knowledgeMetadata{Attributes: attributes, ValidFrom: from, ValidTo: to, Version: version}, nil
}

// parseAttributesJSON accepts an empty string as "not set" and otherwise
// requires a flat object whose values are scalars, matching the search filter.
func parseAttributesJSON(raw string) (json.RawMessage, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil, nil
	}
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	var values map[string]any
	if err := dec.Decode(&values); err != nil {
		return nil, apperr.New(apperr.KindBadRequest, "attributes harus berupa objek JSON")
	}
	if values == nil {
		return nil, nil
	}
	if len(values) > maxMetadataAttributes {
		return nil, apperr.New(apperr.KindBadRequest, "maksimal 10 atribut")
	}
	for key, value := range values {
		if key == "" || len(key) > maxMetadataKeyLen {
			return nil, apperr.New(apperr.KindBadRequest, "nama atribut tidak valid")
		}
		switch value.(type) {
		case string, json.Number, float64, bool, nil:
		default:
			return nil, apperr.New(apperr.KindBadRequest, "atribut %s harus berupa teks, angka, bool, atau null", key)
		}
	}
	compact, err := json.Marshal(values)
	if err != nil {
		return nil, apperr.New(apperr.KindBadRequest, "attributes tidak dapat dibaca")
	}
	return compact, nil
}

func parseOptionalDate(raw, field string) (*time.Time, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil, nil
	}
	at, err := time.Parse("2006-01-02", text)
	if err != nil {
		return nil, apperr.New(apperr.KindBadRequest, "%s harus tanggal YYYY-MM-DD", field)
	}
	return &at, nil
}

func checkWindow(from, to *time.Time) error {
	if from != nil && to != nil && from.After(*to) {
		return apperr.New(apperr.KindBadRequest, "valid_from tidak boleh melewati valid_to")
	}
	return nil
}

// metadataInputFromBody applies the PATCH semantics: nil fields stay as they
// are, an explicit empty date clears that bound, and an empty attributes
// object clears the attribute set.
func metadataInputFromBody(body dtos.KnowledgeMetadataBody) (repositories.KnowledgeMetadataInput, error) {
	var in repositories.KnowledgeMetadataInput
	if len(body.Attributes) > 0 {
		attributes, err := parseAttributesJSON(string(body.Attributes))
		if err != nil {
			return in, err
		}
		if attributes == nil {
			attributes = json.RawMessage(`{}`)
		}
		in.Attributes = attributes
	}
	if (body.ValidFrom == nil) != (body.ValidTo == nil) {
		return in, apperr.New(apperr.KindBadRequest, "valid_from dan valid_to harus dikirim bersama")
	}
	if body.ValidFrom != nil {
		from, err := parseOptionalDate(*body.ValidFrom, "valid_from")
		if err != nil {
			return in, err
		}
		to, err := parseOptionalDate(*body.ValidTo, "valid_to")
		if err != nil {
			return in, err
		}
		if err = checkWindow(from, to); err != nil {
			return in, err
		}
		in.SetWindow = true
		in.ValidFrom, in.ValidTo = from, to
	}
	if body.Version != nil {
		version := strings.TrimSpace(*body.Version)
		if len([]rune(version)) > maxMetadataVersionLen {
			return in, apperr.New(apperr.KindBadRequest, "version maksimum 64 karakter")
		}
		in.Version = &version
	}
	return in, nil
}

// SetMetadata replaces the retrieval metadata of a document and its chunks.
func (h *KnowledgeHandler) SetMetadata(c fiber.Ctx) error {
	uid, tenant, err := knowledgeScope(c)
	if err != nil {
		return err
	}
	id, err := pathID(c, "knowledge document")
	if err != nil {
		return err
	}
	var body dtos.KnowledgeMetadataBody
	if err = lib.ValidateRequestBody(c, &body); err != nil {
		return err
	}
	in, err := metadataInputFromBody(body)
	if err != nil {
		return err
	}
	doc, err := h.repo.SetMetadata(c.Context(), uid, tenant, id, in)
	if err != nil {
		return err
	}
	return dtos.Get(c, doc)
}
