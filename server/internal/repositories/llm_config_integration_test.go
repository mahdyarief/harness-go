//go:build integration

package repositories

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// Provide an isolated database; migrations must already be applied.
func TestLLMConfigIntegration(t *testing.T) {
	dsn := os.Getenv("HSG_LLM_TEST_DSN")
	if dsn == "" {
		t.Skip("set HSG_LLM_TEST_DSN to an isolated migrated database")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	r := &LLMConfigRepository{BaseRepository{DB: db}}
	c := LLMConfig{Endpoint: "https://synthetic.invalid/v1", Model: "synthetic", TimeoutSeconds: 30, MaxTokens: 1024, Enabled: true}
	if err := r.Save(ctx, c, "admin", true); err == nil {
		t.Fatal("enabled without credential")
	}
	c.EncryptedCredential = []byte("synthetic-one")
	if err := r.Save(ctx, c, "admin", false); err != nil {
		t.Fatal(err)
	}
	c.EncryptedCredential = nil
	originalEndpoint := c.Endpoint
	c.Endpoint = "https://another.invalid/v1"
	if err := r.Save(ctx, c, "admin", true); err == nil {
		t.Fatal("credential transferred to a different endpoint")
	}
	c.Endpoint = originalEndpoint
	if err := r.Save(ctx, c, "admin", true); err != nil {
		t.Fatal(err)
	}
	got, err := r.Get(ctx)
	if err != nil || string(got.EncryptedCredential) != "synthetic-one" {
		t.Fatalf("preserve: %v", err)
	}
	c.Endpoint = "https://another.invalid/v1"
	c.EncryptedCredential = []byte("synthetic-two")
	if err := r.Save(ctx, c, "admin", true); err != nil {
		t.Fatalf("replace credential while changing endpoint: %v", err)
	}
	c.EncryptedCredential = []byte("rollback")
	if err := r.Save(ctx, c, " ", false); err == nil {
		t.Fatal("invalid actor accepted")
	}
	got, err = r.Get(ctx)
	if err != nil || string(got.EncryptedCredential) != "synthetic-two" {
		t.Fatalf("rollback: %v", err)
	}
	c.EncryptedCredential = nil
	if err := r.Save(ctx, c, "admin", false); err == nil {
		t.Fatal("enabled removal accepted")
	}
	c.Enabled = false
	if err := r.Save(ctx, c, "admin", false); err != nil {
		t.Fatal(err)
	}
	got, err = r.Get(ctx)
	if err != nil || len(got.EncryptedCredential) != 0 {
		t.Fatalf("remove: %v", err)
	}
	var n int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM llm_config_audit_events WHERE actor = 'admin' AND action = 'save' AND created_at IS NOT NULL").Scan(&n); err != nil || n != 4 {
		t.Fatalf("audit count %d: %v", n, err)
	}
}
