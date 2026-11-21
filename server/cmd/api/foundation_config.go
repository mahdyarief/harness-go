package main

import (
	"encoding/base64"
	"fmt"
	"net"
	"strconv"
	"strings"

	"hsg/internal/config"
)

func readFoundation(getenv func(string) string) (config.Foundation, error) {
	cfg := config.Foundation{TenantDSN: strings.TrimSpace(getenv("TENANT_DB_DSN"))}
	if cfg.TenantDSN == "" {
		return cfg, fmt.Errorf("TENANT_DB_DSN wajib diisi dengan role runtime tenant tanpa bypass RLS")
	}
	if encoded := strings.TrimSpace(getenv("LLM_ENCRYPTION_KEY")); encoded != "" {
		key, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(key) != 32 {
			return cfg, fmt.Errorf("LLM_ENCRYPTION_KEY harus berupa base64 dari 32 byte")
		}
		cfg.LLMEncryptionKey = key
	}
	for _, host := range strings.Split(getenv("LLM_ALLOWED_HOSTS"), ",") {
		host = strings.ToLower(strings.TrimSpace(host))
		if host == "" {
			continue
		}
		if strings.ContainsAny(host, "/:@*?# \\%") || strings.HasSuffix(host, ".") {
			return cfg, fmt.Errorf("LLM_ALLOWED_HOSTS hanya menerima nama host persis tanpa wildcard atau port")
		}
		cfg.LLMAllowedHosts = append(cfg.LLMAllowedHosts, host)
	}
	cfg.EmbeddingWorkerDSN = strings.TrimSpace(getenv("EMBEDDING_WORKER_DB_DSN"))
	if value := strings.TrimSpace(getenv("EMBEDDING_ALLOW_PRIVATE")); value != "" {
		allowed, err := strconv.ParseBool(value)
		if err != nil {
			return cfg, fmt.Errorf("EMBEDDING_ALLOW_PRIVATE harus berupa boolean")
		}
		cfg.EmbeddingAllowPrivate = allowed
	}
	for _, host := range strings.Split(getenv("EMBEDDING_ALLOWED_HOSTS"), ",") {
		host = strings.ToLower(strings.TrimSpace(host))
		if host == "" {
			continue
		}
		if net.ParseIP(host) == nil {
			if len(host) > 253 {
				return cfg, fmt.Errorf("EMBEDDING_ALLOWED_HOSTS hanya menerima host persis tanpa wildcard atau port")
			}
			for _, label := range strings.Split(host, ".") {
				if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
					return cfg, fmt.Errorf("EMBEDDING_ALLOWED_HOSTS hanya menerima host persis tanpa wildcard atau port")
				}
				for _, c := range label {
					if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
						return cfg, fmt.Errorf("EMBEDDING_ALLOWED_HOSTS hanya menerima host persis tanpa wildcard atau port")
					}
				}
			}
		}
		cfg.EmbeddingAllowedHosts = append(cfg.EmbeddingAllowedHosts, host)
	}
	return cfg, nil
}
