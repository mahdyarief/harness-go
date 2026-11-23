ALTER TABLE knowledge_documents
    DROP COLUMN IF EXISTS attributes,
    DROP COLUMN IF EXISTS valid_from,
    DROP COLUMN IF EXISTS valid_to,
    DROP COLUMN IF EXISTS version;