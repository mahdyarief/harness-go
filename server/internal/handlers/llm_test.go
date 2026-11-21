package handlers

import (
	"context"
	"errors"
	"fmt"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"hsg/internal/app"
	"hsg/internal/config"
	"hsg/internal/lib"
	"hsg/internal/lib/ai"
	"hsg/internal/repositories"
)

type memoryLLM struct {
	cfg           repositories.LLMConfig
	saves, audits int
	auditErr      error
}

func (s *memoryLLM) Get(context.Context) (repositories.LLMConfig, error) { return s.cfg, nil }
func (s *memoryLLM) Save(_ context.Context, c repositories.LLMConfig, actor string, keep bool) error {
	if actor == "" {
		return errors.New("missing actor")
	}
	if keep && len(c.EncryptedCredential) == 0 {
		c.EncryptedCredential = s.cfg.EncryptedCredential
	}
	s.cfg = c
	s.saves++
	return nil
}
func (s *memoryLLM) AuditTest(context.Context, string, bool) error { s.audits++; return s.auditErr }
func harness(t *testing.T) (*llmHandler, *memoryLLM, *fiber.App) {
	t.Helper()
	s := &memoryLLM{cfg: repositories.LLMConfig{TimeoutSeconds: 30, MaxTokens: 1024}}
	h := &llmHandler{app: &app.Application{Foundation: config.Foundation{
		LLMEncryptionKey: make([]byte, 32), LLMAllowedHosts: []string{"api.example.com"}}}, store: s}
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
func request(t *testing.T, a *fiber.App, method, path, body string) (int, string) {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	res, err := a.Test(r)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

const configBody = `{"endpoint":"https://api.example.com/v1/chat/completions","model":"demo","timeout_seconds":30,"max_tokens":100,"enabled":true,"credential":"write-only-secret"}`

func TestLLMTokenBoundaries(t *testing.T) {
	for _, n := range []int{0, 1, 16384, 16385} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			_, s, a := harness(t)
			status, _ := request(t, a, "PUT", "/config", fmt.Sprintf(`{"timeout_seconds":30,"max_tokens":%d}`, n))
			want := 200
			if n < 1 || n > 16384 {
				want = 422
			}
			if status != want {
				t.Fatalf("status = %d, want %d", status, want)
			}
			if want == 200 && s.cfg.MaxTokens != n {
				t.Fatal("token value not saved")
			}
		})
	}
}

func TestLLMConfigCredentialLifecycle(t *testing.T) {
	h, s, a := harness(t)
	status, _ := request(t, a, "PUT", "/config", configBody)
	if status != 200 {
		t.Fatal(status)
	}
	key, _ := h.key()
	p, err := ai.OpenSecret(key, s.cfg.EncryptedCredential)
	if err != nil || string(p) != "write-only-secret" {
		t.Fatal(err)
	}
	status, body := request(t, a, "GET", "/config", "")
	if status != 200 || strings.Contains(body, "write-only-secret") || strings.Contains(body, "encrypted_credential") || !strings.Contains(body, `"has_credential":true`) {
		t.Fatal(status, body)
	}
	status, _ = request(t, a, "PUT", "/config", strings.Replace(configBody, `,"credential":"write-only-secret"`, "", 1))
	if status != 200 {
		t.Fatal(status)
	}
	status, _ = request(t, a, "PUT", "/config", strings.Replace(strings.Replace(configBody, `"enabled":true`, `"enabled":false`, 1), `"credential":"write-only-secret"`, `"clear_credential":true`, 1))
	if status != 200 || len(s.cfg.EncryptedCredential) > 0 {
		t.Fatal(status)
	}
}
func TestLLMLanguagePersistence(t *testing.T) {
	_, s, a := harness(t)
	s.cfg.Language = "id-ID"
	if status, _ := request(t, a, "PUT", "/config", configBody); status != 200 || s.cfg.Language != "id-ID" {
		t.Fatalf("omitted language reset stored value: status=%d language=%q", status, s.cfg.Language)
	}
	status, _ := request(t, a, "PUT", "/config", strings.Replace(configBody, `"model":"demo"`, `"model":"demo","language":"en"`, 1))
	if status != 200 || s.cfg.Language != "en" {
		t.Fatalf("language not saved: status=%d language=%q", status, s.cfg.Language)
	}
	if status, body := request(t, a, "GET", "/config", ""); status != 200 || !strings.Contains(body, `"language":"en"`) {
		t.Fatal(status, body)
	}
	if status, _ := request(t, a, "PUT", "/config", configBody); status != 200 || s.cfg.Language != "en" {
		t.Fatalf("omitted language reset stored value: status=%d language=%q", status, s.cfg.Language)
	}
}

func TestLLMRejectionsAndConnection(t *testing.T) {
	h, s, a := harness(t)
	if code, _ := request(t, a, "POST", "/test", `{}`); code != 409 {
		t.Fatal(code)
	}
	for _, v := range []string{strings.Replace(configBody, "api.example.com", "evil.example.com", 1), strings.Replace(configBody, "https://", "http://", 1), strings.Replace(configBody, `"credential":"write-only-secret"`, `"credential":"bad\nsecret"`, 1)} {
		if code, _ := request(t, a, "PUT", "/config", v); code != 422 {
			t.Fatal(code)
		}
	}
	h.app.Foundation.LLMEncryptionKey = nil
	if code, _ := request(t, a, "PUT", "/config", configBody); code != 503 {
		t.Fatal(code)
	}
	h.app.Foundation.LLMEncryptionKey = make([]byte, 32)
	if code, _ := request(t, a, "PUT", "/config", configBody); code != 200 {
		t.Fatal(code)
	}
	if code, _ := request(t, a, "POST", "/test", `{"patient":"private"}`); code != 400 {
		t.Fatal(code)
	}
	ran := false
	h.runConnection = func(context.Context, *ai.Gateway) error {
		ran = true
		return nil
	}
	s.auditErr = errors.New("audit failed")
	if code, _ := request(t, a, "POST", "/test", `{}`); code != 500 || ran {
		t.Fatal(code, ran)
	}
	s.auditErr = nil
	if code, body := request(t, a, "POST", "/test", `{}`); code != 200 || !ran || !strings.Contains(body, `"status":"completed"`) || strings.Contains(body, "quote") {
		t.Fatal(code, body)
	}
	for _, tc := range []struct {
		name   string
		err    error
		code   int
		status string
	}{
		{"transport", errors.New("sensitive provider error"), 502, "failed"},
		{"timeout", context.DeadlineExceeded, 504, "timeout"},
		{"auth", &ai.ProviderError{Status: 401}, 502, "provider_auth_failed"},
		{"access", &ai.ProviderError{Status: 403}, 502, "provider_auth_failed"},
		{"quota", &ai.ProviderError{Status: 429}, 502, "rate_limited"},
		{"provider", &ai.ProviderError{Status: 500}, 502, "failed"},
		{"output", ai.ErrOutput, 502, "invalid_response"},
		{"size", ai.ErrResponseTooLarge, 502, "invalid_response"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h.runConnection = func(context.Context, *ai.Gateway) error { return tc.err }
			code, body := request(t, a, "POST", "/test", `{}`)
			if code != tc.code || !strings.Contains(body, `"status":"`+tc.status+`"`) || strings.Contains(body, "sensitive") {
				t.Fatal(code, body)
			}
		})
	}
}
