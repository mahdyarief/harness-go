ALTER TABLE llm_config ADD COLUMN language TEXT NOT NULL DEFAULT 'id-ID' CHECK (length(language) <= 64);
