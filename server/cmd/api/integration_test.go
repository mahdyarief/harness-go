package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	authulamodels "github.com/Authula/authula/models"
	"github.com/gofiber/fiber/v3"

	"hsg/internal/app"
	"hsg/internal/config"
	"hsg/internal/handlers"
	"hsg/internal/lib"
	"hsg/internal/lib/blob"
	"hsg/internal/lib/constant"
	"hsg/internal/lib/kb"
	"hsg/internal/repositories"
	"hsg/internal/seeders"
)

// TestIntegration runs the boot sequence and the auth flow against a real
// PostgreSQL 18. It needs TEST_DB_DSN, pointing at a database it may wipe:
//
//	TEST_DB_DSN=postgres://postgres:postgres@localhost:5432/hsg_test?sslmode=disable go test ./cmd/api
func TestIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set")
	}

	const (
		superEmail = "admin@example.com"
		superPass  = "super-user-password"
		userEmail  = "user@example.com"
		userPass   = "plain-user-password"
	)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Config{
		App: config.ConfigApp{
			Env:    config.EnvDevelopment,
			Port:   8080,
			Name:   "hsg-test",
			Secret: "0123456789abcdef0123456789abcdef",
			// Fiber's test connection comes from 0.0.0.0; trusting it lets the
			// test send X-Forwarded-For the way a reverse proxy would.
			TrustedProxies: "0.0.0.0",
		},
		DB: config.ConfigDB{DSN: dsn, MaxOpenConns: 5, MaxIdleConns: 5},
		Auth: config.ConfigAuth{
			BaseURL:      "http://localhost:8080",
			EmailFrom:    "noreply@example.com",
			ResendAPIKey: "re_test",
		},
	}

	// Start from nothing; this also runs every down migration.
	migrateDB, err := lib.OpenDB(dsn)
	if err != nil {
		t.Fatal(err)
	}
	m, err := lib.NewMigrator(migrateDB)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.Version(); err == nil {
		if err := m.Down(); err != nil {
			t.Fatalf("down: %v", err)
		}
	}
	m.Close()

	if err := migrateUp(&cfg.DB, logger); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	db, err := connectDB(&cfg.DB)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	repos := repositories.New(db)
	mirror := mirrorUser(&repos.User)
	auth := newAuth(cfg, mirror, logger)
	defer auth.CloseSystems()
	defer auth.ClosePlugins()

	ctx := context.Background()
	superUser := seeders.SuperUserSeeder{
		DB:       db,
		Auth:     auth.CoreServices(),
		Promote:  promoteSuperUser(&repos.User, mirror),
		Email:    superEmail,
		Password: "the-first-password",
	}
	// Twice: seeding must be repeatable.
	for range 2 {
		if err := seedAll(db, superUser, logger); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	// A plain user, created the way a verified sign-up ends up.
	services := auth.CoreServices()
	user, err := services.UserService.Create(ctx, "Plain User", userEmail, true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := services.PasswordService.Hash(userPass)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := services.AccountService.Create(ctx, user.ID, userEmail, authulamodels.AuthProviderEmail.String(), &hash); err != nil {
		t.Fatal(err)
	}

	// Authula's tables live in their own schema, and the mirror follows them.
	var authUsers int
	if err := db.QueryRow(`SELECT count(*) FROM authula.users`).Scan(&authUsers); err != nil || authUsers != 2 {
		t.Fatalf("authula.users: count=%d err=%v", authUsers, err)
	}
	roles := map[string]string{}
	rows, err := db.Query(`SELECT email, role FROM public.users`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var email, role string
		if err := rows.Scan(&email, &role); err != nil {
			t.Fatal(err)
		}
		roles[email] = role
	}
	rows.Close()
	if roles[superEmail] != constant.RoleSuperUser || roles[userEmail] != constant.RoleUser || len(roles) != 2 {
		t.Fatalf("public.users roles = %v", roles)
	}

	// Reconcile restores a mirror row that went missing and keeps the roles.
	if _, err := db.Exec(`DELETE FROM public.users WHERE email = $1`, userEmail); err != nil {
		t.Fatal(err)
	}
	if err := repos.User.Reconcile(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	var role string
	if err := db.QueryRow(`SELECT role FROM public.users WHERE email = $1`, superEmail).Scan(&role); err != nil || role != constant.RoleSuperUser {
		t.Fatalf("super user role after reconcile: %q err=%v", role, err)
	}
	if err := db.QueryRow(`SELECT role FROM public.users WHERE email = $1`, userEmail).Scan(&role); err != nil || role != constant.RoleUser {
		t.Fatalf("plain user after reconcile: %q err=%v", role, err)
	}

	// Changing SUPER_USER's password resets the account on the next seed.
	superUser.Password = superPass
	if err := superUser.Seed(); err != nil {
		t.Fatalf("reseed: %v", err)
	}

	tenantDB := tenantTestPool(t, db, dsn)
	testApp := &app.Application{
		Config: cfg, Logger: logger, DB: db, TenantDB: tenantDB, Repositories: repos, Auth: auth,
		Foundation: config.Foundation{
			LLMEncryptionKey: bytes.Repeat([]byte{7}, 32),
			LLMAllowedHosts:  []string{"api.example.com"},
		},
	}
	store, err := blob.NewDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	extractors := kb.NewRegistry(kb.NewMarkdownExtractor(), kb.NewTextExtractor(), kb.NewCSVExtractor(), kb.NewDOCXExtractor(), kb.NewXLSXExtractor())
	server, err := newServer(testApp, handlers.NewKnowledgeHandler(testApp, store, extractors))
	if err != nil {
		t.Fatal(err)
	}

	// cookies holds the Set-Cookie pairs of the last answer.
	var cookies string

	// call sends one request and decodes the JSON answer.
	call := func(method, path, token string, body any, headers ...string) (int, map[string]any) {
		t.Helper()
		var payload io.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			payload = bytes.NewReader(raw)
		}
		req := httptest.NewRequest(method, path, payload)
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		for i := 0; i+1 < len(headers); i += 2 {
			req.Header.Set(headers[i], headers[i+1])
		}
		resp, err := server.Test(req, fiber.TestConfig{Timeout: 30 * time.Second})
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		cookies = ""
		for _, c := range resp.Cookies() {
			cookies += c.Name + "=" + c.Value + "; "
		}
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}

	signIn := func(email, password string) map[string]any {
		t.Helper()
		status, out := call(http.MethodPost, "/api/v1/auth/email-password/sign-in", "", map[string]string{"email": email, "password": password})
		if status != http.StatusOK {
			t.Fatalf("sign in %s: %d %v", email, status, out)
		}
		for _, key := range []string{"user", "session", "access_token", "refresh_token", "expires_at", "expires_in"} {
			if out[key] == nil {
				t.Fatalf("sign in %s: no %q in %v", email, key, out)
			}
		}
		return out
	}

	if status, _ := call(http.MethodPost, "/api/v1/auth/email-password/sign-in", "", map[string]string{"email": superEmail, "password": "the-first-password"}); status == http.StatusOK {
		t.Fatal("the replaced super user password still signs in")
	}
	super := signIn(superEmail, superPass)
	superToken := super["access_token"].(string)
	superCookie := cookies
	if superCookie == "" {
		t.Fatal("sign in set no session cookie")
	}

	// A session without tokens can get a pair: by signing in again, or from
	// POST /token with the session cookie.
	if status, out := call(http.MethodPost, "/api/v1/auth/email-password/sign-in", "", map[string]string{"email": superEmail, "password": superPass}, "Cookie", superCookie); status != http.StatusOK || out["access_token"] == nil || out["refresh_token"] == nil {
		t.Fatalf("repeated sign in: %d %v", status, out)
	}
	status, out := call(http.MethodPost, "/api/v1/auth/token", "", nil, "Cookie", superCookie)
	if status != http.StatusOK || out["access_token"] == nil || out["refresh_token"] == nil || out["expires_in"] == nil {
		t.Fatalf("token with the cookie: %d %v", status, out)
	}
	if status, out := call(http.MethodPost, "/api/v1/auth/token/refresh", "", map[string]any{"refresh_token": out["refresh_token"]}); status != http.StatusOK {
		t.Fatalf("refresh of an issued token: %d %v", status, out)
	}
	// An access token cannot mint new pairs, or a stolen one would outlive its 15 minutes.
	if status, _ := call(http.MethodPost, "/api/v1/auth/token", superToken, nil); status != http.StatusUnauthorized {
		t.Fatalf("token with a bearer only: %d, want 401", status)
	}
	if status, _ := call(http.MethodPost, "/api/v1/auth/token", "", nil); status != http.StatusUnauthorized {
		t.Fatalf("token without a session: %d, want 401", status)
	}

	// Signing up does not sign in, and an unverified address cannot sign in:
	// otherwise anyone could hold a session for someone else's email.
	const strangerEmail = "stranger@example.com"
	status, out = call(http.MethodPost, "/api/v1/auth/email-password/sign-up", "", map[string]string{"name": "Stranger", "email": strangerEmail, "password": "stranger-password"})
	if status != http.StatusCreated || out["access_token"] != nil || cookies != "" {
		t.Fatalf("sign up: %d %v, cookies %q; want 201 without a session", status, out, cookies)
	}
	if status, out := call(http.MethodPost, "/api/v1/auth/email-password/sign-in", "", map[string]string{"email": strangerEmail, "password": "stranger-password"}); status != http.StatusUnauthorized {
		t.Fatalf("unverified sign in: %d %v, want 401", status, out)
	}
	userToken := signIn(userEmail, userPass)["access_token"].(string)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		status, out := call(method, "/api/v1/chat/sessions", userToken, map[string]string{})
		if status != http.StatusForbidden || out["message"] != "Akun Anda belum memiliki keanggotaan tenant. Hubungi admin untuk menambahkan akun Anda ke tenant." {
			t.Fatalf("chat tanpa membership: %d %v", status, out)
		}
	}
	if status, out := call(http.MethodGet, "/api/v1/chat/sessions", superToken, nil); status != http.StatusOK {
		t.Fatalf("platform list tanpa membership: %d %v", status, out)
	}
	status, platformChat := call(http.MethodPost, "/api/v1/chat/sessions", superToken, map[string]string{})
	if status != http.StatusCreated {
		t.Fatalf("platform create tanpa membership: %d %v", status, platformChat)
	}
	platformData := platformChat["data"].(map[string]any)
	if platformData["tenant_id"] != nil || platformData["scope"] != "platform-personal" {
		t.Fatalf("platform scope: %v", platformData)
	}
	platformPath := "/api/v1/chat/sessions/" + platformData["id"].(string)
	if status, out := call(http.MethodPost, platformPath+"/turn", superToken, map[string]any{"content": "Halo sintetis", "stream": false}); status != http.StatusConflict {
		t.Fatalf("platform turn harus melewati saldo tenant: %d %v", status, out)
	}
	if status, out := call(http.MethodDelete, platformPath, superToken, nil); status != http.StatusOK {
		t.Fatalf("platform delete tanpa membership: %d %v", status, out)
	}
	testTenantHTTP(t, server, db, superToken, userToken, user.ID)
	// Before the LLM is configured, so a turn can only fail closed.
	testChatHTTP(t, call, userToken, superToken)

	for _, endpoint := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/platform/llm-config"},
		{http.MethodPut, "/api/v1/platform/llm-config"},
		{http.MethodPost, "/api/v1/platform/llm-test"},
		{http.MethodGet, "/api/v1/platform/exa-config"},
		{http.MethodPut, "/api/v1/platform/exa-config"},
		{http.MethodPost, "/api/v1/platform/exa-test"},
	} {
		for _, principal := range []struct {
			token string
			want  int
		}{{"", http.StatusUnauthorized}, {userToken, http.StatusForbidden}} {
			if status, _ := call(endpoint.method, endpoint.path, principal.token, nil); status != principal.want {
				t.Fatalf("%s %s: status %d, want %d", endpoint.method, endpoint.path, status, principal.want)
			}
		}
	}
	if status, out := call(http.MethodGet, "/api/v1/platform/llm-config", superToken, nil); status != http.StatusOK {
		t.Fatalf("platform LLM config: %d %v", status, out)
	} else if data, ok := out["data"].(map[string]any); !ok || data["enabled"] != false || data["has_credential"] != false || data["encrypted_credential"] != nil || data["credential"] != nil {
		t.Fatalf("LLM default or credential exposure: %v", out)
	}
	llmConfig := map[string]any{
		"endpoint": "https://api.example.com/v1", "model": "synthetic-model",
		"timeout_seconds": 10, "max_tokens": 128, "enabled": false,
		"credential": "synthetic-test-credential",
	}
	if status, out := call(http.MethodPut, "/api/v1/platform/llm-config", superToken, llmConfig); status != http.StatusOK {
		t.Fatalf("save LLM config: %d %v", status, out)
	}
	if status, out := call(http.MethodGet, "/api/v1/platform/llm-config", superToken, nil); status != http.StatusOK {
		t.Fatalf("read LLM config: %d %v", status, out)
	} else if data, ok := out["data"].(map[string]any); !ok || data["has_credential"] != true || data["credential"] != nil || data["encrypted_credential"] != nil {
		t.Fatalf("credential exposure or missing credential: %v", out)
	}
	delete(llmConfig, "credential")
	llmConfig["endpoint"] = "https://untrusted.example/v1"
	if status, _ := call(http.MethodPut, "/api/v1/platform/llm-config", superToken, llmConfig); status != http.StatusUnprocessableEntity {
		t.Fatalf("untrusted endpoint: %d, want 422", status)
	}
	llmConfig["endpoint"] = "https://api.example.com/v1"
	llmConfig["clear_credential"] = true
	if status, out := call(http.MethodPut, "/api/v1/platform/llm-config", superToken, llmConfig); status != http.StatusOK {
		t.Fatalf("clear credential: %d %v", status, out)
	}
	if status, out := call(http.MethodGet, "/api/v1/platform/llm-config", superToken, nil); status != http.StatusOK || out["data"].(map[string]any)["has_credential"] != false {
		t.Fatalf("credential not cleared: %d %v", status, out)
	}

	// Exa keys are write-only: stored encrypted, reported as a count, never echoed.
	if status, out := call(http.MethodGet, "/api/v1/platform/exa-config", superToken, nil); status != http.StatusOK {
		t.Fatalf("platform Exa config: %d %v", status, out)
	} else if data, ok := out["data"].(map[string]any); !ok || data["enabled"] != false || data["has_keys"] != false || data["key_count"] != float64(0) || data["encrypted_keys"] != nil {
		t.Fatalf("Exa default or key exposure: %v", out)
	}
	exaConfig := map[string]any{"enabled": true, "keys": []string{"synthetic-exa-key-one", "synthetic-exa-key-two"}}
	if status, out := call(http.MethodPut, "/api/v1/platform/exa-config", superToken, exaConfig); status != http.StatusOK {
		t.Fatalf("save Exa config: %d %v", status, out)
	}
	if status, out := call(http.MethodGet, "/api/v1/platform/exa-config", superToken, nil); status != http.StatusOK {
		t.Fatalf("read Exa config: %d %v", status, out)
	} else {
		data := out["data"].(map[string]any)
		if data["enabled"] != true || data["has_keys"] != true || data["key_count"] != float64(2) || data["encrypted_keys"] != nil {
			t.Fatalf("Exa config read model: %v", out)
		}
		encoded, _ := json.Marshal(out)
		if bytes.Contains(encoded, []byte("synthetic-exa-key")) {
			t.Fatalf("Exa keys leaked: %s", encoded)
		}
	}
	// An omitted key list must preserve the stored keys.
	if status, out := call(http.MethodPut, "/api/v1/platform/exa-config", superToken, map[string]any{"enabled": true}); status != http.StatusOK {
		t.Fatalf("preserve Exa keys: %d %v", status, out)
	}
	if status, out := call(http.MethodGet, "/api/v1/platform/exa-config", superToken, nil); status != http.StatusOK || out["data"].(map[string]any)["key_count"] != float64(2) {
		t.Fatalf("Exa keys dropped: %d %v", status, out)
	}
	if status, out := call(http.MethodPut, "/api/v1/platform/exa-config", superToken, map[string]any{"enabled": false, "clear_keys": true}); status != http.StatusOK {
		t.Fatalf("clear Exa keys: %d %v", status, out)
	}
	if status, out := call(http.MethodGet, "/api/v1/platform/exa-config", superToken, nil); status != http.StatusOK || out["data"].(map[string]any)["has_keys"] != false {
		t.Fatalf("Exa keys not cleared: %d %v", status, out)
	}
	// Disabled integration refuses the connectivity test without calling Exa.
	if status, out := call(http.MethodPost, "/api/v1/platform/exa-test", superToken, nil); status != http.StatusConflict {
		t.Fatalf("disabled Exa test: %d %v", status, out)
	}

	if status, out := call(http.MethodGet, "/health", "", nil); status != http.StatusOK {
		t.Fatalf("health: %d %v", status, out)
	}
	if status, out := call(http.MethodGet, "/api/v1/auth/me", superToken, nil); status != http.StatusOK {
		t.Fatalf("me with bearer: %d %v", status, out)
	}
	if status, out := call(http.MethodPost, "/api/v1/auth/token/refresh", "", map[string]any{"refresh_token": super["refresh_token"]}); status != http.StatusOK || out["access_token"] == nil {
		t.Fatalf("refresh: %d %v", status, out)
	}

	// Category writes: super user only.
	category := map[string]string{"name": "Integration"}
	if status, _ := call(http.MethodPost, "/api/v1/categories", "", category); status != http.StatusUnauthorized {
		t.Fatalf("create without auth: %d, want 401", status)
	}
	if status, _ := call(http.MethodPost, "/api/v1/categories", userToken, category); status != http.StatusForbidden {
		t.Fatalf("create as plain user: %d, want 403", status)
	}
	if status, out := call(http.MethodPost, "/api/v1/categories", superToken, category); status != http.StatusCreated {
		t.Fatalf("create as super user: %d %v", status, out)
	}
	if status, out := call(http.MethodGet, "/api/v1/categories?search=Integration", "", nil); status != http.StatusOK || len(out["data"].([]any)) != 1 {
		t.Fatalf("list: %d %v", status, out)
	}

	// Idempotent create: the same key replays the first answer instead of
	// inserting a second row, and the reply is marked as replayed.
	const idempotencyKey = "8e03978e-40d5-43e8-bc93-6894a57f9324"
	retried := map[string]string{"name": "Retried"}
	firstStatus, firstOut := call(http.MethodPost, "/api/v1/categories", superToken, retried, "X-Idempotency-Key", idempotencyKey)
	if firstStatus != http.StatusCreated {
		t.Fatalf("idempotent create: %d %v", firstStatus, firstOut)
	}
	retryStatus, retryOut := call(http.MethodPost, "/api/v1/categories", superToken, retried, "X-Idempotency-Key", idempotencyKey)
	if retryStatus != firstStatus || retryOut["data"].(map[string]any)["id"] != firstOut["data"].(map[string]any)["id"] {
		t.Fatalf("idempotent retry: %d %v, want the first answer %v", retryStatus, retryOut, firstOut)
	}
	if status, out := call(http.MethodGet, "/api/v1/categories?search=Retried", "", nil); status != http.StatusOK || len(out["data"].([]any)) != 1 {
		t.Fatalf("the retry inserted a second row: %d %v", status, out)
	}
	// A malformed key is refused before the write is attempted.
	if status, _ := call(http.MethodPost, "/api/v1/categories", superToken, map[string]string{"name": "Bad"}, "X-Idempotency-Key", "not-a-uuid"); status != http.StatusBadRequest {
		t.Fatalf("malformed idempotency key: %d, want 400", status)
	}

	// A browser request from another site is refused before it is authenticated.
	if status, _ := call(http.MethodPost, "/api/v1/categories", superToken, category, "Sec-Fetch-Site", "cross-site", "Origin", "https://evil.example"); status != http.StatusForbidden {
		t.Fatalf("cross-site create: %d, want 403", status)
	}

	// Signing out revokes the access token.
	if status, out := call(http.MethodPost, "/api/v1/auth/sign-out", superToken, map[string]any{}); status != http.StatusOK {
		t.Fatalf("sign out: %d %v", status, out)
	}
	if status, _ := call(http.MethodPost, "/api/v1/categories", superToken, category); status != http.StatusUnauthorized {
		t.Fatalf("create after sign out: %d, want 401", status)
	}

	// Rotating SUPER_USER ends the sessions opened with the old password.
	superToken = signIn(superEmail, superPass)["access_token"].(string)
	superCookie = cookies
	superUser.Password = "the-rotated-password"
	if err := superUser.Seed(); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	for _, headers := range [][]string{{"Cookie", superCookie}, {"Authorization", "Bearer " + superToken}} {
		if status, _ := call(http.MethodGet, "/api/v1/auth/me", "", nil, headers...); status != http.StatusUnauthorized {
			t.Fatalf("me with %s after rotation: %d, want 401", headers[0], status)
		}
	}

	// Behind a proxy the limiter keys on the client the proxy saw, not on
	// the header prefix the client chose, so changing it does not reset the count.
	status = 0
	for i := range authRateLimit + 1 {
		status, _ = call(http.MethodPost, "/api/v1/auth/email-password/sign-in", "", map[string]string{"email": "nobody@example.com", "password": "wrong-password"},
			"X-Forwarded-For", fmt.Sprintf("10.0.0.%d, 203.0.113.9", i))
	}
	if status != http.StatusTooManyRequests {
		t.Fatalf("sign in %d times with spoofed prefixes: last %d, want 429", authRateLimit+1, status)
	}
}
