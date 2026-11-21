ALTER TABLE knowledge_documents
    ADD COLUMN extraction_metadata jsonb;

ALTER TABLE knowledge_documents
    ADD CONSTRAINT knowledge_documents_extraction_metadata_check CHECK (
        extraction_metadata IS NULL OR COALESCE((
            jsonb_typeof(extraction_metadata) = 'object'
            AND extraction_metadata ?& ARRAY['status', 'extractor_version', 'warnings', 'coverage']
            AND extraction_metadata->>'status' IN ('complete', 'limited')
            AND jsonb_typeof(extraction_metadata->'extractor_version') = 'string'
            AND length(extraction_metadata->>'extractor_version') BETWEEN 1 AND 100
            AND jsonb_typeof(extraction_metadata->'warnings') = 'array'
            AND jsonb_typeof(extraction_metadata->'coverage') = 'array'
        ), false)
    );
