DROP TABLE IF EXISTS knowledge_chunks;
DROP TABLE IF EXISTS knowledge_documents;
DROP FUNCTION IF EXISTS enforce_knowledge_chunk_scope();
DROP FUNCTION IF EXISTS reject_knowledge_document_rescope();
DROP FUNCTION IF EXISTS knowledge_can_write(uuid);
DROP FUNCTION IF EXISTS knowledge_can_read(uuid);
