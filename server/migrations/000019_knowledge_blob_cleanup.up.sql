-- Keep only opaque storage keys, never filenames or file contents. A trigger
-- makes metadata deletion and cleanup intent one transaction, including CASCADE
-- deletes and commits whose acknowledgement never reaches the API.
CREATE TABLE public.knowledge_blob_cleanup (
    storage_key text PRIMARY KEY CHECK (storage_key ~ '^[0-9a-f]{64}$'),
    queued_at timestamptz NOT NULL DEFAULT now()
);
REVOKE ALL ON TABLE public.knowledge_blob_cleanup FROM PUBLIC;

-- Runtime roles can delete documents but cannot access the owner-only queue.
-- Qualify every object and exclude public/pg_temp from name resolution.
CREATE FUNCTION public.enqueue_knowledge_blob_cleanup() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog
AS $$
BEGIN
    INSERT INTO public.knowledge_blob_cleanup (storage_key)
    VALUES (OLD.storage_key)
    ON CONFLICT (storage_key) DO NOTHING;
    RETURN OLD;
END;
$$;
REVOKE ALL ON FUNCTION public.enqueue_knowledge_blob_cleanup() FROM PUBLIC;

CREATE TRIGGER knowledge_documents_blob_cleanup
AFTER DELETE ON public.knowledge_documents
FOR EACH ROW EXECUTE FUNCTION public.enqueue_knowledge_blob_cleanup();
