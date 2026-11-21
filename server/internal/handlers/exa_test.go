package handlers

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"hsg/internal/app"
	"hsg/internal/config"
	"hsg/internal/lib"
	"hsg/internal/lib/ai"
	"hsg/internal/lib/exa"
	"hsg/internal/repositories"
)

type memoryExa struct {
	cfg           repositories.ExaConfig
	saves, audits int
	keepSeen      bool
}

func (s *memoryExa) Get(context.Context) (repositories.ExaConfig, error) { return s.cfg, nil }
func (s *memoryExa) Save(_ context.Context, c repositories.ExaConfig, actor string, keep bool) error {
	if actor == "" {
		return errors.New("missing actor")
	}
	if keep {
		s.keepSeen = true
		if len(c.EncryptedKeys) == 0 {
			c.EncryptedKeys = s.cfg.EncryptedKeys
			c.KeyCount = s.cfg.KeyCount
		}
	}
	s.cfg = c
	s.saves++
	return nil
}
func (s *memoryExa) AuditTest(context.Context, string, bool) error { s.audits++; return nil }

func exaHarness(t *testing.T) (*exaHandler, *memoryExa, *fiber.App) {
	t.Helper()
	s := &memoryExa{}
	h := &exaHandler{app: &app.Application{Foundation: config.Foundation{LLMEncryptionKey: make([]byte, 32)}}, store: s}
	a := fiber.New()
	a.Use(func(c fiber.Ctx) error {
		lib.ContextSetUID(c, uuid.MustParse("4f54815f-a2d7-4610-b0dd-024b86491885"))
		return c.Next()
	})
	a.Get("/config", h.Get)
	a.Put("/config", h.Put)
	a.Post("/test", h.Test)
	return h, s, a
}

func TestExaPutStoresSealedKeysAndNeverEchoesThem(t *testing.T) {
	_, store, a := exaHarness(t)
	status, body := request(t, a, "PUT", "/config", `{"enabled":true,"keys":["exa-key-one","exa-key-two"]}`)
	if status != 200 {
		t.Fatalf("status = %d body = %s", status, body)
	}
	if store.saves != 1 || store.cfg.KeyCount != 2 || len(store.cfg.EncryptedKeys) == 0 {
		t.Fatalf("store = %+v", store.cfg)
	}
	plain, err := ai.OpenSecret(make([]byte, 32), store.cfg.EncryptedKeys)
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != "exa-key-one\nexa-key-two" {
		t.Fatalf("plaintext = %q", plain)
	}

	status, body = request(t, a, "GET", "/config", "")
	if status != 200 {
		t.Fatalf("status = %d body = %s", status, body)
	}
	for _, leak := range []string{"exa-key-one", "exa-key-two", "encrypted_keys"} {
		if strings.Contains(body, leak) {
			t.Fatalf("response leaked %q: %s", leak, body)
		}
	}
	if !strings.Contains(body, `"has_keys":true`) || !strings.Contains(body, `"key_count":2`) {
		t.Fatalf("body = %s", body)
	}
}

func TestExaPutKeylessEnable(t *testing.T) {
	_, store, a := exaHarness(t)
	status, body := request(t, a, "PUT", "/config", `{"enabled":true}`)
	if status != 200 {
		t.Fatalf("status = %d body = %s", status, body)
	}
	if store.cfg.KeyCount != 0 || len(store.cfg.EncryptedKeys) != 0 {
		t.Fatalf("store = %+v", store.cfg)
	}
	if !store.keepSeen {
		t.Fatal("omitted keys should preserve existing keys")
	}
}

func TestExaPutClearKeys(t *testing.T) {
	_, store, a := exaHarness(t)
	store.cfg = repositories.ExaConfig{Enabled: true, EncryptedKeys: []byte{1}, KeyCount: 1}
	status, body := request(t, a, "PUT", "/config", `{"enabled":true,"clear_keys":true}`)
	if status != 200 {
		t.Fatalf("status = %d body = %s", status, body)
	}
	if store.cfg.KeyCount != 0 || len(store.cfg.EncryptedKeys) != 0 {
		t.Fatalf("store = %+v", store.cfg)
	}
}

func TestExaPutRejectsConflictingAndInvalidKeys(t *testing.T) {
	_, store, a := exaHarness(t)
	cases := []string{
		`{"enabled":true,"clear_keys":true,"keys":["a"]}`,
		`{"enabled":true,"keys":["bad key"]}`,
		`{"enabled":true,"keys":["ok","ok"]}`,
	}
	for _, body := range cases[:2] {
		if status, resp := request(t, a, "PUT", "/config", body); status != 422 {
			t.Fatalf("body %s: status = %d resp = %s", body, status, resp)
		}
	}
	// Duplicates are collapsed rather than rejected.
	if status, resp := request(t, a, "PUT", "/config", cases[2]); status != 200 {
		t.Fatalf("status = %d resp = %s", status, resp)
	}
	if store.cfg.KeyCount != 1 {
		t.Fatalf("key count = %d", store.cfg.KeyCount)
	}
}

func TestExaPutEmptyKeyListPreservesStoredKeys(t *testing.T) {
	_, store, a := exaHarness(t)
	store.cfg = repositories.ExaConfig{Enabled: true, EncryptedKeys: []byte{1}, KeyCount: 1}
	if status, resp := request(t, a, "PUT", "/config", `{"enabled":true,"keys":[]}`); status != 200 {
		t.Fatalf("status = %d resp = %s", status, resp)
	}
	if store.cfg.KeyCount != 1 || len(store.cfg.EncryptedKeys) != 1 {
		t.Fatalf("stored keys were dropped: %+v", store.cfg)
	}
}

func TestExaPutRejectsUnknownFields(t *testing.T) {
	_, _, a := exaHarness(t)
	if status, _ := request(t, a, "PUT", "/config", `{"enabled":true,"endpoint":"https://evil"} `); status != 400 {
		t.Fatalf("status = %d", status)
	}
}

func TestExaTestMapsFailures(t *testing.T) {
	key := make([]byte, 32)
	sealed, err := ai.SealSecret(key, []byte("exa-key"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		enabled bool
		err     error
		status  int
		want    string
	}{
		{"ok", true, nil, 200, `"status":"completed"`},
		{"timeout", true, context.DeadlineExceeded, 504, `"status":"timeout"`},
		{"auth", true, &exa.StatusError{Status: 401}, 502, `"status":"provider_auth_failed"`},
		{"rate", true, &exa.StatusError{Status: 429}, 502, `"status":"rate_limited"`},
		{"invalid", true, exa.ErrTool, 502, `"status":"invalid_response"`},
		{"failed", true, errors.New("network"), 502, `"status":"failed"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &memoryExa{cfg: repositories.ExaConfig{Enabled: tc.enabled, EncryptedKeys: sealed, KeyCount: 1}}
			h := &exaHandler{
				app:   &app.Application{Foundation: config.Foundation{LLMEncryptionKey: key}},
				store: s,
				runSearch: func(context.Context, *exa.Client) error {
					return tc.err
				},
			}
			a := fiber.New()
			a.Use(func(c fiber.Ctx) error {
				lib.ContextSetUID(c, uuid.MustParse("4f54815f-a2d7-4610-b0dd-024b86491885"))
				return c.Next()
			})
			a.Post("/test", h.Test)
			status, body := request(t, a, "POST", "/test", "")
			if status != tc.status || !strings.Contains(body, tc.want) {
				t.Fatalf("status = %d body = %s", status, body)
			}
			if s.audits != 2 {
				t.Fatalf("audits = %d", s.audits)
			}
		})
	}
}

func TestExaTestRefusesDisabled(t *testing.T) {
	_, _, a := exaHarness(t)
	if status, body := request(t, a, "POST", "/test", ""); status != 409 {
		t.Fatalf("status = %d body = %s", status, body)
	}
}
