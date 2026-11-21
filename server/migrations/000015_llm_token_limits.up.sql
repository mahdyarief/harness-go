ALTER TABLE llm_config DROP CONSTRAINT llm_config_max_tokens_check;
ALTER TABLE llm_config ALTER COLUMN max_tokens SET DEFAULT 16384;

-- Preserve credentials, model, endpoint and every other singleton setting.
UPDATE llm_config SET max_tokens = 16384 WHERE singleton = TRUE;

ALTER TABLE llm_config ADD CONSTRAINT llm_config_max_tokens_check
    CHECK (max_tokens BETWEEN 1 AND 16384);
