DROP INDEX IF EXISTS knowledge_chunks_attributes_idx;

ALTER TABLE knowledge_chunks
    DROP COLUMN IF EXISTS attributes,
    DROP COLUMN IF EXISTS valid_from,
    DROP COLUMN IF EXISTS valid_to,
    DROP COLUMN IF EXISTS version;
