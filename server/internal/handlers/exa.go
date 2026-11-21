package handlers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"hsg/internal/app"
	"hsg/internal/dtos"
	"hsg/internal/lib"
	"hsg/internal/lib/ai"
	"hsg/internal/lib/exa"
	"hsg/internal/repositories"
)

type exaStore interface {
	Get(context.Context) (repositories.ExaConfig, error)
	Save(context.Context, repositories.ExaConfig, string, bool) error
	AuditTest(context.Context, string, bool) error
}

type exaHandler struct {
	app       *app.Application
	store     exaStore
	runSearch func(context.Context, *exa.Client) error
}

func (h *exaHandler) repo() exaStore {
	if h.store != nil {
		return h.store
	}
	return &h.app.Repositories.ExaConfig
}

type exaBody struct {
	Enabled   bool      `json:"enabled"`
	Keys      *[]string `json:"keys"`
	ClearKeys bool      `json:"clear_keys"`
}

// exaKeys decrypts the stored key blob. A missing blob is keyless mode, not an
// error; only a corrupt blob is.
func exaKeys(app *app.Application, cfg repositories.ExaConfig) ([]string, error) {
	if len(cfg.EncryptedKeys) == 0 {
		return nil, nil
	}
	key := app.Foundation.LLMEncryptionKey
	if len(key) != 32 {
		return nil, fiber.NewError(503, "LLM encryption key unavailable")
	}
	plain, err := ai.OpenSecret(key, cfg.EncryptedKeys)
	if err != nil {
		return nil, fiber.NewError(503, "stored Exa keys unavailable")
	}
	lines := strings.Split(string(plain), "\n")
	keys := make([]string, 0, len(lines))
	for _, line := range lines {
		if k := strings.TrimSpace(line); k != "" {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

// newExaClient builds the search client from the stored configuration.
func newExaClient(app *app.Application, cfg repositories.ExaConfig) (*exa.Client, []string, error) {
	keys, err := exaKeys(app, cfg)
	if err != nil {
		return nil, nil, err
	}
	client, err := exa.New(exa.Config{Keys: keys, Timeout: 45 * time.Second})
	if err != nil {
		return nil, nil, fiber.NewError(503, "Exa configuration unavailable")
	}
	return client, keys, nil
}

// normalizeExaKeys trims, drops blanks, rejects duplicates and any key that
// carries whitespace or control characters. Keys are opaque tokens, so nothing
// is echoed back in an error.
func normalizeExaKeys(raw []string) ([]string, error) {
	if len(raw) > exa.MaxKeys {
		return nil, fiber.NewError(422, fmt.Sprintf("maksimal %d kunci Exa", exa.MaxKeys))
	}
	seen := make(map[string]struct{}, len(raw))
	keys := make([]string, 0, len(raw))
	for _, item := range raw {
		k := strings.TrimSpace(item)
		if k == "" {
			continue
		}
		if len(k) > 512 || strings.ContainsAny(k, " \t\r\n") {
			return nil, fiber.NewError(422, "format kunci Exa tidak valid")
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		keys = append(keys, k)
	}
	return keys, nil
}

func (h *exaHandler) Get(c fiber.Ctx) error {
	cfg, err := h.repo().Get(c.Context())
	if err != nil {
		return err
	}
	return dtos.OK(c, struct {
		repositories.ExaConfig
		HasKeys bool `json:"has_keys"`
	}{cfg, cfg.KeyCount > 0})
}

func (h *exaHandler) Put(c fiber.Ctx) error {
	uid, err := lib.ContextGetUID(c)
	if err != nil {
		return fiber.ErrUnauthorized
	}
	var body exaBody
	if err := strictLLMBody(c, &body); err != nil {
		return err
	}
	if body.ClearKeys && body.Keys != nil {
		return fiber.NewError(422, "operasi kunci Exa bertentangan")
	}
	// Absent or empty keys preserve the stored list. Only an explicit
	// clear_keys removes it, so a partial form save can never wipe secrets.
	keep := !body.ClearKeys
	cfg := repositories.ExaConfig{Enabled: body.Enabled}
	if body.Keys != nil {
		keys, err := normalizeExaKeys(*body.Keys)
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			encryptionKey := h.app.Foundation.LLMEncryptionKey
			if len(encryptionKey) != 32 {
				return fiber.NewError(503, "LLM encryption key unavailable")
			}
			cfg.EncryptedKeys, err = ai.SealSecret(encryptionKey, []byte(strings.Join(keys, "\n")))
			if err != nil {
				return fiber.NewError(503, "key encryption failed")
			}
			cfg.KeyCount = len(keys)
			keep = false
		}
	}
	if err := h.repo().Save(c.Context(), cfg, uid.String(), keep); err != nil {
		return err
	}
	return dtos.Message(c, 200, "Konfigurasi Exa disimpan")
}

func (h *exaHandler) Test(c fiber.Ctx) error {
	uid, err := lib.ContextGetUID(c)
	if err != nil {
		return fiber.ErrUnauthorized
	}
	// Only an absent body or empty object is accepted. Never accept queries or
	// provider overrides from the client.
	if len(c.Body()) > 0 {
		var empty struct{}
		if err := strictLLMBody(c, &empty); err != nil {
			return err
		}
	}
	cfg, err := h.repo().Get(c.Context())
	if err != nil {
		return err
	}
	if !cfg.Enabled {
		return fiber.NewError(409, "Exa web access is disabled")
	}
	client, _, err := newExaClient(h.app, cfg)
	if err != nil {
		return err
	}
	if err := h.repo().AuditTest(c.Context(), uid.String(), false); err != nil {
		return err
	}
	run := h.runSearch
	if run == nil {
		run = func(ctx context.Context, c *exa.Client) error {
			_, err := c.Search(ctx, "HSG connectivity test", 1)
			return err
		}
	}
	searchErr := run(c.Context(), client)
	if err := h.repo().AuditTest(c.Context(), uid.String(), true); err != nil {
		return err
	}
	status, outcome := 200, "completed"
	if searchErr != nil {
		status, outcome = 502, "failed"
		var statusErr *exa.StatusError
		switch {
		case errors.Is(searchErr, context.DeadlineExceeded):
			status, outcome = 504, "timeout"
		case errors.As(searchErr, &statusErr):
			switch statusErr.Status {
			case 401, 403:
				outcome = "provider_auth_failed"
			case 429:
				outcome = "rate_limited"
			}
		case errors.Is(searchErr, exa.ErrOutput), errors.Is(searchErr, exa.ErrResponseTooLarge), errors.Is(searchErr, exa.ErrTool):
			outcome = "invalid_response"
		}
	}
	return dtos.Response(c, status, fiber.Map{"status": outcome}, nil, "")
}
