-- Knowledge metadata: free-form attributes, a validity window, and a version
-- label. The meaning of attributes (regulation number, article, effective
-- year) belongs to the consumer; the retrieval layer only filters and cites.
ALTER TABLE knowledge_documents
    ADD COLUMN attributes jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN valid_from date,
    ADD COLUMN valid_to date,
    ADD COLUMN version text NOT NULL DEFAULT '' CHECK (length(version) <= 64);

CREATE INDEX knowledge_documents_attributes_idx ON knowledge_documents USING gin (attributes);
CREATE INDEX knowledge_documents_validity_idx ON knowledge_documents (valid_from, valid_to);