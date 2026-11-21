-- Pending jobs must be drained before intentionally rolling back this feature.
DROP TRIGGER IF EXISTS knowledge_documents_blob_cleanup ON public.knowledge_documents;
DROP FUNCTION IF EXISTS public.enqueue_knowledge_blob_cleanup();
DROP TABLE IF EXISTS public.knowledge_blob_cleanup;
