package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"hsg/internal/lib"
)

// seedEmbedding bootstraps the knowledge_embedding_config singleton, the
// bge-m3 profile, and the sealed credential so the worker runs on first deploy
// without a manual API call. It reuses the same AES-GCM envelope and AAD as
// handlers/embeddingCredential, so the running API can decrypt the credential.
func seedEmbedding() error {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(os.Getenv("LLM_ENCRYPTION_KEY")))
	if err != nil || len(key) != 32 {
		return fmt.Errorf("LLM_ENCRYPTION_KEY must be base64 of 32 bytes")
	}
	endpoint := os.Getenv("EMBEDDING_ENDPOINT")
	revision := strings.ToLower(strings.TrimSpace(os.Getenv("EMBEDDING_REVISION")))
	credential := os.Getenv("EMBEDDING_API_KEY")
	if endpoint == "" || revision == "" || credential == "" {
		return fmt.Errorf("EMBEDDING_ENDPOINT, EMBEDDING_REVISION, EMBEDDING_API_KEY must be set")
	}
	if len(revision) != 40 {
		return fmt.Errorf("EMBEDDING_REVISION must be a 40-hex commit")
	}

	digest := sha256.Sum256([]byte("BAAI/bge-m3|" + revision + "|1024|l2|cosine|kb-chunk-v1"))
	fingerprint := hex.EncodeToString(digest[:])

	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	aad := "hsg/knowledge-embedding/credential/v1/config/singleton|" + endpoint + "|" + fingerprint
	sealed := gcm.Seal(nil, nonce, []byte(credential), []byte(aad))
	encrypted := append(append([]byte{1}, nonce...), sealed...)

	db, err := lib.OpenDB(os.Getenv("DB_DSN"))
	if err != nil {
		return err
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	// The owner login is a superuser here, so RLS does not block this write.
	// The profile is immutable by trigger, so an existing row is reused as-is.
	var profile string
	err = db.QueryRowContext(ctx, `INSERT INTO knowledge_embedding_profiles(revision) VALUES($1) ON CONFLICT DO NOTHING RETURNING id::text`, revision).Scan(&profile)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == sql.ErrNoRows {
		if err = db.QueryRowContext(ctx, `SELECT id::text FROM knowledge_embedding_profiles WHERE revision=$1`, revision).Scan(&profile); err != nil {
			return err
		}
	}

	if _, err = db.ExecContext(ctx,
		`UPDATE knowledge_embedding_config
		 SET endpoint=$1, profile_id=$2, enabled=true, encrypted_credential=$3,
		     generation=generation+1, backfill_after=NULL, backfill_done=false, updated_at=now()
		 WHERE singleton`,
		endpoint, profile, encrypted,
	); err != nil {
		return err
	}

	fmt.Printf("embedding config enabled: endpoint=%s revision=%s fingerprint=%s\n", endpoint, revision, fingerprint)
	return nil
}
