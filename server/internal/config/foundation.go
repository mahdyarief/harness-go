package config

type Foundation struct {
	TenantDSN             string   `json:"-"`
	LLMEncryptionKey      []byte   `json:"-"`
	LLMAllowedHosts       []string `json:"-"`
	EmbeddingWorkerDSN    string   `json:"-"`
	EmbeddingAllowedHosts []string `json:"-"`
	EmbeddingAllowPrivate bool     `json:"-"`
}
