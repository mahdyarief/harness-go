package handlers

import (
	"bytes"
	"strings"
	"testing"

	"hsg/internal/lib/ai"
	"hsg/internal/models"
)

func TestEmbeddingCredentialPurposeAndConfigurationBinding(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	cfg := models.KnowledgeEmbeddingConfig{Endpoint: "https://embed.example.com/v1/embeddings", Revision: strings.Repeat("a", 40)}
	sealed, err := embeddingCredential(key, cfg, []byte("secret"), true)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := embeddingCredential(key, cfg, sealed, false)
	if err != nil || string(plain) != "secret" {
		t.Fatal("round trip", err)
	}
	other := cfg
	other.Endpoint = "https://other.example.com/v1/embeddings"
	if _, err = embeddingCredential(key, other, sealed, false); err == nil {
		t.Fatal("endpoint substitution accepted")
	}
	other = cfg
	other.Revision = strings.Repeat("b", 40)
	if _, err = embeddingCredential(key, other, sealed, false); err == nil {
		t.Fatal("profile substitution accepted")
	}
	if _, err = ai.OpenSecret(key, sealed); err == nil {
		t.Fatal("embedding secret accepted as chat secret")
	}
	chat, err := ai.SealSecret(key, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = embeddingCredential(key, cfg, chat, false); err == nil {
		t.Fatal("chat secret accepted as embedding secret")
	}
	if _, err = embeddingCredential(nil, cfg, sealed, false); err == nil {
		t.Fatal("missing key accepted")
	}
}
