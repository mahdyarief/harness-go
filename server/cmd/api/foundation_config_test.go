package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hsg/internal/app"
	"hsg/internal/config"
	"hsg/internal/models"
)

func TestReadFoundationEnv(t *testing.T) {
	t.Setenv("TENANT_DB_DSN", "postgres://runtime/db")
	t.Setenv("LLM_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 32))))
	t.Setenv("LLM_ALLOWED_HOSTS", "api.example.com")
	cfg, err := readFoundationEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TenantDSN != "postgres://runtime/db" || len(cfg.LLMEncryptionKey) != 32 || len(cfg.LLMAllowedHosts) != 1 || cfg.LLMAllowedHosts[0] != "api.example.com" {
		t.Fatal("konfigurasi environment tidak diteruskan dengan benar")
	}
}

func TestReadEmbeddingFoundation(t *testing.T) {
	for _, tc := range []struct {
		name, hosts, private string
		valid                bool
	}{
		{"default", "", "", true},
		{"internal", "embed.internal,127.0.0.1,::1", "true", true},
		{"hyphenated hostname", "embedding-service", "false", true},
		{"empty label", "embed..internal", "false", false},
		{"leading hyphen", "-embed.internal", "false", false},
		{"trailing hyphen", "embed-.internal", "false", false},
		{"trailing dot", "embed.internal.", "false", false},
		{"underscore", "embed_service", "false", false},
		{"long label", strings.Repeat("a", 64) + ".internal", "false", false},
		{"long hostname", strings.Repeat("a.", 127) + "a", "false", false},
		{"wildcard", "*.internal", "false", false},
		{"url", "https://embed.internal", "false", false},
		{"port", "embed.internal:8080", "false", false},
		{"invalid boolean", "embed.internal", "maybe", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{"TENANT_DB_DSN": "postgres://runtime/db", "EMBEDDING_ALLOWED_HOSTS": tc.hosts, "EMBEDDING_ALLOW_PRIVATE": tc.private}
			cfg, err := readFoundation(func(k string) string { return env[k] })
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
			if err == nil && tc.name == "default" && (cfg.EmbeddingAllowPrivate || cfg.EmbeddingWorkerDSN != "" || len(cfg.EmbeddingAllowedHosts) != 0) {
				t.Fatal("embedding harus nonaktif secara default")
			}
			if err == nil && tc.name == "internal" && (!cfg.EmbeddingAllowPrivate || len(cfg.EmbeddingAllowedHosts) != 3) {
				t.Fatal("izin jaringan embedding tidak diteruskan")
			}
		})
	}
}

func TestEmbeddingRuntimeFailsClosed(t *testing.T) {
	application := &app.Application{}
	stop, err := startKnowledgeEmbeddingWorker(application)
	if err != nil || stop == nil {
		t.Fatalf("worker tanpa DSN harus nonaktif: %v", err)
	}
	stop()
	application.Foundation.EmbeddingWorkerDSN = "postgres://invalid.invalid/db"
	if _, err = startKnowledgeEmbeddingWorker(application); err == nil {
		t.Fatal("worker tanpa kunci harus ditolak sebelum koneksi database")
	}
	cfg := models.KnowledgeEmbeddingConfig{Endpoint: "http://127.0.0.1:1", Model: models.KnowledgeEmbeddingModel, Revision: strings.Repeat("a", 40), Dimensions: 1024}
	application.Foundation = config.Foundation{LLMAllowedHosts: []string{"127.0.0.1"}}
	if err = newKnowledgeEmbeddingProbe(application)(context.Background(), cfg, "synthetic-secret"); err == nil {
		t.Fatal("izin provider LLM tidak boleh membuka jaringan embedding")
	}
}

func TestEmbeddingRuntimeProbeContract(t *testing.T) {
	revision := strings.Repeat("a", 40)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" || r.Header.Get("Authorization") != "Bearer synthetic-secret" || r.Header.Get("X-Embedding-Revision") != revision {
			t.Error("kontrak request probe tidak sesuai")
		}
		vector := make([]float32, 1024)
		vector[0] = 1
		w.Header().Set("X-Embedding-Revision", revision)
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "model": models.KnowledgeEmbeddingModel, "data": []any{map[string]any{"object": "embedding", "index": 0, "embedding": vector}}})
	}))
	defer server.Close()
	application := &app.Application{Foundation: config.Foundation{EmbeddingAllowedHosts: []string{"127.0.0.1"}, EmbeddingAllowPrivate: true}}
	cfg := models.KnowledgeEmbeddingConfig{Endpoint: server.URL, Model: models.KnowledgeEmbeddingModel, Revision: revision, Dimensions: 1024}
	if err := newKnowledgeEmbeddingProbe(application)(context.Background(), cfg, "synthetic-secret"); err != nil {
		t.Fatal(err)
	}
}

func TestReadFoundation(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 32)))
	for _, tc := range []struct {
		name  string
		dsn   string
		key   string
		hosts string
		valid bool
	}{
		{"minimal", "postgres://runtime/db", "", "", true},
		{"configured", "postgres://runtime/db", key, "api.example.com", true},
		{"missing tenant", "", key, "", false},
		{"short key", "postgres://runtime/db", "eA==", "", false},
		{"invalid key", "postgres://runtime/db", "invalid", "", false},
		{"wildcard", "postgres://runtime/db", key, "*.example.com", false},
		{"url", "postgres://runtime/db", key, "https://example.com", false},
		{"port", "postgres://runtime/db", key, "example.com:443", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{"TENANT_DB_DSN": tc.dsn, "LLM_ENCRYPTION_KEY": tc.key, "LLM_ALLOWED_HOSTS": tc.hosts}
			_, err := readFoundation(func(k string) string { return env[k] })
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
}
