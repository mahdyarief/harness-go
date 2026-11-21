ALTER TABLE llm_config DROP CONSTRAINT llm_config_max_tokens_check;
ALTER TABLE llm_config ALTER COLUMN max_tokens SET DEFAULT 1024;
ALTER TABLE llm_config ADD CONSTRAINT llm_config_max_tokens_check
    CHECK (max_tokens BETWEEN 1 AND 131072);
