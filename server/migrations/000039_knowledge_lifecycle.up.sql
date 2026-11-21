-- Dokumen yang baru diunggah lewat aplikasi menunggu review sebelum bisa
-- dipakai retrieval agen. Default 'published' menjaga dokumen existing, seeder,
-- dan fixture integrasi (yang menyisipkan dokumen langsung) tetap aktif.
ALTER TABLE knowledge_documents
    ADD COLUMN status text NOT NULL DEFAULT 'published',
    ADD CONSTRAINT knowledge_documents_status_check
        CHECK (status IN ('draft', 'published'));

CREATE INDEX knowledge_documents_status_idx
    ON knowledge_documents (status, tenant_id);
