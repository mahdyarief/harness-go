package dtos

import (
	"strings"
	"testing"
)

func TestKnowledgeEmbeddingProfileValidation(t *testing.T) {
	valid := KnowledgeEmbeddingUpdate{Endpoint: " https://embed.example.com/v1/embeddings ", Revision: strings.Repeat("A", 40)}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	if valid.Model != "BAAI/bge-m3" || valid.Dimensions != 1024 || valid.Revision != strings.Repeat("a", 40) {
		t.Fatal("defaults/normalization failed")
	}
	fingerprint := KnowledgeEmbeddingFingerprint(valid.Revision)
	if len(fingerprint) != 64 || fingerprint != KnowledgeEmbeddingFingerprint(strings.Repeat("A", 40)) {
		t.Fatal("unstable fingerprint")
	}
	cases := []KnowledgeEmbeddingUpdate{valid, valid, valid, valid, valid}
	cases[0].Revision = "main"
	cases[1].Dimensions = 768
	cases[2].Model = "other"
	cases[3].Endpoint = "https://u:p@embed.example.com"
	secret := "bad\nsecret"
	cases[4].Credential = &secret
	for i, c := range cases {
		if err := c.Validate(); err == nil {
			t.Errorf("invalid case %d accepted", i)
		}
	}
	httpInternal := KnowledgeEmbeddingUpdate{Endpoint: "http://embedding:8080/v1/embeddings", Revision: strings.Repeat("A", 40)}
	if err := httpInternal.Validate(); err != nil {
		t.Fatalf("internal http endpoint rejected: %v", err)
	}
}
