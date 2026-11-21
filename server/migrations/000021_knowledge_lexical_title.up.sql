-- Keep title text local: generated search vectors cannot read another table.
ALTER TABLE knowledge_chunks ADD COLUMN title text NOT NULL DEFAULT '';

UPDATE knowledge_chunks c SET title = d.title
FROM knowledge_documents d WHERE d.id = c.document_id;

CREATE FUNCTION sync_knowledge_chunk_title() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    -- Serialize chunk writes with document renames so a concurrent insert
    -- cannot leave a stale title. Invoker rights preserve document RLS.
    SELECT d.title INTO NEW.title FROM knowledge_documents d
    WHERE d.id = NEW.document_id FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'knowledge_chunks.document_id does not reference a visible document';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER knowledge_chunks_title
    BEFORE INSERT OR UPDATE OF document_id, title ON knowledge_chunks
    FOR EACH ROW EXECUTE FUNCTION sync_knowledge_chunk_title();

CREATE FUNCTION sync_knowledge_document_title() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    UPDATE knowledge_chunks SET title = NEW.title WHERE document_id = NEW.id;
    RETURN NEW;
END;
$$;

CREATE TRIGGER knowledge_documents_title
    AFTER UPDATE OF title ON knowledge_documents
    FOR EACH ROW WHEN (OLD.title IS DISTINCT FROM NEW.title)
    EXECUTE FUNCTION sync_knowledge_document_title();

ALTER TABLE knowledge_chunks ALTER COLUMN search SET EXPRESSION AS (
    setweight(to_tsvector('indonesian'::regconfig, coalesce(title, '')), 'A') ||
    setweight(to_tsvector('indonesian'::regconfig, coalesce(heading_path, '')), 'B') ||
    setweight(to_tsvector('indonesian'::regconfig, coalesce(content, '')), 'D')
);
