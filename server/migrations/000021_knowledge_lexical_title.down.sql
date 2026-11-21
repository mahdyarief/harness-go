-- Restore the deployed heading/content weighting without deleting knowledge.
DROP TRIGGER knowledge_documents_title ON knowledge_documents;
DROP FUNCTION sync_knowledge_document_title();
DROP TRIGGER knowledge_chunks_title ON knowledge_chunks;
DROP FUNCTION sync_knowledge_chunk_title();

ALTER TABLE knowledge_chunks ALTER COLUMN search SET EXPRESSION AS (
    setweight(to_tsvector('indonesian'::regconfig, coalesce(heading_path, '')), 'A') ||
    setweight(to_tsvector('indonesian'::regconfig, coalesce(content, '')), 'B')
);
ALTER TABLE knowledge_chunks DROP COLUMN title;
