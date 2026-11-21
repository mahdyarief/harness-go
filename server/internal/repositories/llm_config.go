package repositories

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// LLMConfig contains the global provider configuration. EncryptedCredential is
// ciphertext supplied by the service, never plaintext, and is not JSON data.
type LLMConfig struct {
	Endpoint            string    `json:"endpoint"`
	Model               string    `json:"model"`
	Language            string    `json:"language"`
	TimeoutSeconds      int       `json:"timeout_seconds"`
	MaxTokens           int       `json:"max_tokens"`
	Enabled             bool      `json:"enabled"`
	EncryptedCredential []byte    `json:"-"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// LLMConfigRepository uses the starter's shared database handle.
type LLMConfigRepository struct {
	BaseRepository
}

// AuditTest records metadata only; never persist prompts, responses or secrets.
func (r *LLMConfigRepository) AuditTest(ctx context.Context, actor string, finished bool) error {
	action := "test_started"
	if finished {
		action = "test_finished"
	}
	_, err := r.DB.ExecContext(ctx, `INSERT INTO llm_config_audit_events (actor, action) VALUES ($1, $2)`, actor, action)
	return err
}

// Get returns the singleton, including ciphertext for internal service use.
// If the migration's singleton row is absent it returns sql.ErrNoRows.
func (r *LLMConfigRepository) Get(ctx context.Context) (LLMConfig, error) {
	var config LLMConfig
	err := r.queryRowContext(ctx, `SELECT endpoint, model, language, timeout_seconds, max_tokens,
		enabled, encrypted_credential, updated_at FROM llm_config WHERE singleton = TRUE`).Scan(
		&config.Endpoint, &config.Model, &config.Language, &config.TimeoutSeconds, &config.MaxTokens,
		&config.Enabled, &config.EncryptedCredential, &config.UpdatedAt,
	)
	return config, err
}

// Save replaces the configuration and records actor metadata in one transaction.
// Nonempty ciphertext always replaces the credential. When ciphertext is empty,
// keepExistingCredential selects preservation (true) or removal (false).
// The database rejects an enabled configuration without a stored credential.
// Encrypt and authenticate the credential before calling Save; this repository
// never encrypts, logs, or stores plaintext. Do not pass secrets in actor.
func (r *LLMConfigRepository) Save(ctx context.Context, config LLMConfig, actor string, keepExistingCredential bool) error {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var credential any
	if len(config.EncryptedCredential) > 0 {
		credential = config.EncryptedCredential
	}
	result, err := tx.ExecContext(ctx, `UPDATE llm_config SET
		endpoint = $1, model = $2, language = $3, timeout_seconds = $4, max_tokens = $5,
		enabled = $6, encrypted_credential = CASE
			WHEN $7::bytea IS NOT NULL THEN $7::bytea
			WHEN $8 THEN encrypted_credential ELSE NULL END,
		updated_at = now() WHERE singleton = TRUE
		AND ($7::bytea IS NOT NULL OR NOT $8 OR encrypted_credential IS NULL OR endpoint = $1)`, config.Endpoint, config.Model, config.Language,
		config.TimeoutSeconds, config.MaxTokens, config.Enabled, credential, keepExistingCredential)
	if err != nil {
		return fmt.Errorf("save LLM config: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("save LLM config: singleton row missing")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO llm_config_audit_events (actor, action) VALUES ($1, 'save')`, actor); err != nil {
		return fmt.Errorf("audit LLM config: %w", err)
	}
	// audit_logs is RLS-forced, and this repository runs on the owner pool, so
	// the insert policy still needs app.user_id pinned on this transaction.
	actorID, err := uuid.Parse(actor)
	if err != nil {
		return fmt.Errorf("audit LLM config: invalid actor: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `SELECT set_config('app.user_id', $1, true)`, actorID.String()); err != nil {
		return fmt.Errorf("audit LLM config: %w", err)
	}
	if err = auditAppend(ctx, tx, actorID, "super_user", ActionLLMConfigUpdated, "llm_config",
		nil, nil, map[string]any{
			"endpoint":        config.Endpoint,
			"model":           config.Model,
			"language":        config.Language,
			"timeout_seconds": config.TimeoutSeconds,
			"max_tokens":      config.MaxTokens,
			"enabled":         config.Enabled,
			"credential_set":  len(config.EncryptedCredential) > 0 || keepExistingCredential,
		}); err != nil {
		return fmt.Errorf("audit LLM config: %w", err)
	}
	return tx.Commit()
}
