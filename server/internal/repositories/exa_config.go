package repositories

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ExaConfig is the global Exa web-search configuration. EncryptedKeys is one
// sealed blob containing newline-separated API keys, never plaintext and never
// JSON data. A nil blob means keyless mode: the public MCP endpoint is used
// without an Authorization header.
type ExaConfig struct {
	Enabled       bool      `json:"enabled"`
	EncryptedKeys []byte    `json:"-"`
	KeyCount      int       `json:"key_count"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// ExaConfigRepository uses the starter's shared database handle.
type ExaConfigRepository struct {
	BaseRepository
}

// AuditTest records metadata only; never persist keys, queries or responses.
func (r *ExaConfigRepository) AuditTest(ctx context.Context, actor string, finished bool) error {
	action := "test_started"
	if finished {
		action = "test_finished"
	}
	_, err := r.DB.ExecContext(ctx, `INSERT INTO exa_config_audit_events (actor, action) VALUES ($1, $2)`, actor, action)
	return err
}

// Get returns the singleton, including ciphertext for internal service use.
func (r *ExaConfigRepository) Get(ctx context.Context) (ExaConfig, error) {
	var config ExaConfig
	err := r.queryRowContext(ctx, `SELECT enabled, encrypted_keys, key_count, updated_at
		FROM exa_config WHERE singleton = TRUE`).Scan(
		&config.Enabled, &config.EncryptedKeys, &config.KeyCount, &config.UpdatedAt,
	)
	return config, err
}

// Save replaces the configuration and records actor metadata in one transaction.
// Nonempty ciphertext always replaces the key blob. When ciphertext is empty,
// keepExistingKeys selects preservation (true) or removal (false). Enabling the
// integration without keys is valid: that is the keyless mode.
// Encrypt the keys before calling Save; this repository never encrypts, logs, or
// stores plaintext. Do not pass secrets in actor.
func (r *ExaConfigRepository) Save(ctx context.Context, config ExaConfig, actor string, keepExistingKeys bool) error {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var keys any
	if len(config.EncryptedKeys) > 0 {
		keys = config.EncryptedKeys
	}
	result, err := tx.ExecContext(ctx, `UPDATE exa_config SET
		enabled = $1, encrypted_keys = CASE
			WHEN $2::bytea IS NOT NULL THEN $2::bytea
			WHEN $3 THEN encrypted_keys ELSE NULL END,
		key_count = CASE
			WHEN $2::bytea IS NOT NULL THEN $4
			WHEN $3 THEN key_count ELSE 0 END,
		updated_at = now() WHERE singleton = TRUE`, config.Enabled, keys, keepExistingKeys, config.KeyCount)
	if err != nil {
		return fmt.Errorf("save Exa config: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("save Exa config: singleton row missing")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO exa_config_audit_events (actor, action) VALUES ($1, 'save')`, actor); err != nil {
		return fmt.Errorf("audit Exa config: %w", err)
	}
	// audit_logs is RLS-forced, and this repository runs on the owner pool, so
	// the insert policy still needs app.user_id pinned on this transaction.
	actorID, err := uuid.Parse(actor)
	if err != nil {
		return fmt.Errorf("audit Exa config: invalid actor: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `SELECT set_config('app.user_id', $1, true)`, actorID.String()); err != nil {
		return fmt.Errorf("audit Exa config: %w", err)
	}
	if err = auditAppend(ctx, tx, actorID, "super_user", ActionExaConfigUpdated, "exa_config",
		nil, nil, map[string]any{
			"enabled":   config.Enabled,
			"key_count": config.KeyCount,
		}); err != nil {
		return fmt.Errorf("audit Exa config: %w", err)
	}
	return tx.Commit()
}
