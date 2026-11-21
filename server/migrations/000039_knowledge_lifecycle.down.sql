-- Rollback tidak boleh mengubah dokumen draft menjadi aktif diam-diam:
-- terbitkan atau hapus dulu dokumen berstatus draft sebelum menurunkan migrasi.
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM knowledge_documents WHERE status = 'draft') THEN
        RAISE EXCEPTION 'draft knowledge documents must be published or deleted before rollback';
    END IF;
END $$;

DROP INDEX knowledge_documents_status_idx;
ALTER TABLE knowledge_documents
    DROP CONSTRAINT knowledge_documents_status_check,
    DROP COLUMN status;
