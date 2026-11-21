ALTER TABLE knowledge_documents
    DROP CONSTRAINT knowledge_documents_extraction_metadata_check,
    DROP COLUMN extraction_metadata;
