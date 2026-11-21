package dtos

import (
	"crypto/sha256"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"hsg/internal/lib/apperr"
)

type KnowledgeEmbeddingUpdate struct {
	Endpoint        string  `json:"endpoint"`
	Model           string  `json:"model"`
	Revision        string  `json:"revision"`
	Dimensions      int     `json:"dimensions"`
	Enabled         bool    `json:"enabled"`
	Credential      *string `json:"credential"`
	ClearCredential bool    `json:"clear_credential"`
}

func (b *KnowledgeEmbeddingUpdate) Validate() error {
	b.Endpoint = strings.TrimSpace(b.Endpoint)
	if b.Model == "" {
		b.Model = "BAAI/bge-m3"
	}
	if b.Dimensions == 0 {
		b.Dimensions = 1024
	}
	b.Revision = strings.ToLower(strings.TrimSpace(b.Revision))
	if b.Model != "BAAI/bge-m3" || b.Dimensions != 1024 || !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(b.Revision) {
		return apperr.New(apperr.KindBadRequest, "Profil embedding harus bge-m3 dense 1024 dengan revision commit 40 hex")
	}
	u, err := url.Parse(b.Endpoint)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(b.Endpoint) > 2048 {
		return apperr.New(apperr.KindBadRequest, "Endpoint embedding tidak valid")
	}
	if b.Credential != nil && (b.ClearCredential || len(*b.Credential) > 4096 || strings.ContainsAny(*b.Credential, "\r\n")) {
		return apperr.New(apperr.KindBadRequest, "Credential embedding tidak valid")
	}
	return nil
}

func KnowledgeEmbeddingFingerprint(revision string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte("BAAI/bge-m3|"+strings.ToLower(strings.TrimSpace(revision))+"|1024|l2|cosine|kb-chunk-v1")))
}
