package repositories

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLLMConfigCredentialIsNotJSON(t *testing.T) {
	config := LLMConfig{Model: "synthetic", EncryptedCredential: []byte("internal-ciphertext")}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"credential", "internal-ciphertext", "aW50ZXJuYWwtY2lwaGVydGV4dA=="} {
		if strings.Contains(strings.ToLower(string(data)), strings.ToLower(forbidden)) {
			t.Fatalf("JSON exposed credential: %s", data)
		}
	}
	if err := json.Unmarshal([]byte(`{"encrypted_credential":"YXR0YWNrZXI=","EncryptedCredential":"YXR0YWNrZXI="}`), &config); err != nil {
		t.Fatal(err)
	}
	if string(config.EncryptedCredential) != "internal-ciphertext" {
		t.Fatal("JSON input overwrote internal ciphertext")
	}
}
