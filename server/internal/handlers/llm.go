package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"hsg/internal/app"
	"hsg/internal/dtos"
	"hsg/internal/lib"
	"hsg/internal/lib/ai"
	"hsg/internal/repositories"
)

type llmStore interface {
	Get(context.Context) (repositories.LLMConfig, error)
	Save(context.Context, repositories.LLMConfig, string, bool) error
	AuditTest(context.Context, string, bool) error
}

type llmHandler struct {
	app           *app.Application
	store         llmStore
	runConnection func(context.Context, *ai.Gateway) error
}

func (h *llmHandler) repo() llmStore {
	if h.store != nil {
		return h.store
	}
	return &h.app.Repositories.LLMConfig
}

type llmBody struct {
	Endpoint        string  `json:"endpoint"`
	Model           string  `json:"model"`
	Language        string  `json:"language"`
	TimeoutSeconds  int     `json:"timeout_seconds"`
	MaxTokens       int     `json:"max_tokens"`
	Enabled         bool    `json:"enabled"`
	Credential      *string `json:"credential"`
	ClearCredential bool    `json:"clear_credential"`
}

// Reject unknown fields, null and trailing JSON. No request body enters a log.
func strictLLMBody(c fiber.Ctx, dst any) error {
	body := bytes.TrimSpace(c.Body())
	if len(body) == 0 || len(body) > 16384 || body[0] != '{' {
		return fiber.NewError(400, "invalid JSON object")
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if d.Decode(dst) != nil || d.Decode(new(any)) != io.EOF {
		return fiber.NewError(400, "invalid JSON object")
	}
	return nil
}
func (h *llmHandler) key() ([]byte, error) {
	k := h.app.Foundation.LLMEncryptionKey
	if len(k) != 32 {
		return nil, fiber.NewError(503, "LLM encryption key unavailable")
	}
	return k, nil
}
func (h *llmHandler) gateway(c repositories.LLMConfig, credential string) (*ai.Gateway, error) {
	hosts := h.app.Foundation.LLMAllowedHosts
	return ai.NewGateway(ai.Config{Endpoint: c.Endpoint, Model: c.Model, APIKey: credential,
		AllowedHosts: hosts, Timeout: time.Duration(c.TimeoutSeconds) * time.Second,
		MaxTokens: c.MaxTokens, MaxResponseBytes: ai.MaxResponseBytes})
}
func (h *llmHandler) Get(c fiber.Ctx) error {
	cfg, err := h.repo().Get(c.Context())
	if err != nil {
		return err
	}
	return dtos.OK(c, struct {
		repositories.LLMConfig
		HasCredential bool `json:"has_credential"`
	}{cfg, len(cfg.EncryptedCredential) > 0})
}
func (h *llmHandler) Put(c fiber.Ctx) error {
	uid, err := lib.ContextGetUID(c)
	if err != nil {
		return fiber.ErrUnauthorized
	}
	var body llmBody
	if err := strictLLMBody(c, &body); err != nil {
		return err
	}
	if body.ClearCredential && body.Credential != nil {
		return fiber.NewError(422, "credential operations conflict")
	}
	if len(body.Endpoint) > 2048 || len(body.Language) > 64 || body.TimeoutSeconds < 1 || body.TimeoutSeconds > 300 || body.MaxTokens < 1 || body.MaxTokens > ai.MaxOutputTokens {
		return fiber.NewError(422, "invalid LLM configuration")
	}
	cfg := repositories.LLMConfig{Endpoint: body.Endpoint, Model: body.Model, TimeoutSeconds: body.TimeoutSeconds, MaxTokens: body.MaxTokens, Enabled: body.Enabled}
	old, err := h.repo().Get(c.Context())
	if err != nil {
		return err
	}
	// The language is edited in a separate form; an omitted value must keep
	// the stored preference rather than reset it.
	if strings.TrimSpace(body.Language) != "" {
		cfg.Language = strings.TrimSpace(body.Language)
	} else {
		cfg.Language = old.Language
	}
	keep := body.Credential == nil && !body.ClearCredential
	var credential string
	var key []byte
	if body.Credential != nil || body.Enabled {
		key, err = h.key()
		if err != nil {
			return err
		}
	}
	if body.Credential != nil {
		credential = *body.Credential
		if strings.TrimSpace(credential) == "" {
			return fiber.NewError(422, "credential must not be empty; use clear_credential")
		}
	} else if keep && body.Enabled {
		plain, e := ai.OpenSecret(key, old.EncryptedCredential)
		if e != nil {
			return fiber.NewError(422, "stored credential unavailable; replace credential")
		}
		credential = string(plain)
	}
	if keep && len(old.EncryptedCredential) > 0 && old.Endpoint != cfg.Endpoint {
		return fiber.NewError(422, "replace or clear credential when changing endpoint")
	}
	validationKey := credential
	if validationKey == "" {
		validationKey = "validation-only"
	}
	if cfg.Enabled && (body.ClearCredential || credential == "") {
		return fiber.NewError(422, "credential required to enable LLM")
	}
	if cfg.Endpoint != "" || cfg.Model != "" || cfg.Enabled || body.Credential != nil {
		if _, err := h.gateway(cfg, validationKey); err != nil {
			if errors.Is(err, ai.ErrHostNotAllowed) {
				return fiber.NewError(422, "host endpoint tidak diizinkan; tambahkan ke LLM_ALLOWED_HOSTS di konfigurasi server")
			}
			return fiber.NewError(422, "konfigurasi LLM tidak valid; periksa endpoint, model, dan kredensial")
		}
	}
	if body.Credential != nil {
		cfg.EncryptedCredential, err = ai.SealSecret(key, []byte(credential))
		if err != nil {
			return fiber.NewError(503, "credential encryption failed")
		}
	}
	if err := h.repo().Save(c.Context(), cfg, uid.String(), keep); err != nil {
		return err
	}
	return dtos.Message(c, 200, "LLM configuration saved")
}
func (h *llmHandler) Test(c fiber.Ctx) error {
	uid, err := lib.ContextGetUID(c)
	if err != nil {
		return fiber.ErrUnauthorized
	}
	// Only an absent body or empty object is accepted. Never accept claims,
	// prompts, patient data, provider overrides or arbitrary tool arguments.
	if len(bytes.TrimSpace(c.Body())) > 0 {
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
		return fiber.NewError(409, "LLM is disabled")
	}
	key, err := h.key()
	if err != nil {
		return err
	}
	secret, err := ai.OpenSecret(key, cfg.EncryptedCredential)
	if err != nil {
		return fiber.NewError(503, "stored credential unavailable")
	}
	gateway, err := h.gateway(cfg, string(secret))
	if err != nil {
		return fiber.NewError(503, "LLM configuration unavailable")
	}
	// Record the attempt before any external call; fail closed if audit fails.
	if err := h.repo().AuditTest(c.Context(), uid.String(), false); err != nil {
		return err
	}
	run := h.runConnection
	if run == nil {
		run = func(ctx context.Context, g *ai.Gateway) error { return g.CheckConnection(ctx) }
	}
	connectionErr := run(c.Context(), gateway)
	if err := h.repo().AuditTest(c.Context(), uid.String(), true); err != nil {
		return err
	}
	status, outcome := 200, "completed"
	if connectionErr != nil {
		status, outcome = 502, "failed"
		var providerErr *ai.ProviderError
		switch {
		case errors.Is(connectionErr, context.DeadlineExceeded):
			status, outcome = 504, "timeout"
		case errors.As(connectionErr, &providerErr):
			switch providerErr.Status {
			case 401, 403:
				outcome = "provider_auth_failed"
			case 429:
				outcome = "rate_limited"
			}
		case errors.Is(connectionErr, ai.ErrOutput), errors.Is(connectionErr, ai.ErrResponseTooLarge):
			outcome = "invalid_response"
		}
	}
	return dtos.Response(c, status, fiber.Map{"status": outcome}, nil, "")
}
