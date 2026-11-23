-- Chunks carry the same retrieval metadata as their document. Existing rows
-- default to an empty attribute set and an open validity window.
ALTER TABLE knowledge_chunks
    ADD COLUMN attributes jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN valid_from date,
    ADD COLUMN valid_to date,
    ADD COLUMN version text NOT NULL DEFAULT '' CHECK (length(version) <= 64);

CREATE INDEX knowledge_chunks_attributes_idx ON knowledge_chunks USING gin (attributes);
