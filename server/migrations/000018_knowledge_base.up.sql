-- Knowledge base: uploaded reference documents split into searchable chunks.
--
-- Scope is carried by tenant_id. NULL means a GLOBAL document (uploaded by a
-- super_user, visible to every signed-in user, in tenant chat and platform
-- chat alike). A value means a TENANT document (uploaded by that tenant's
-- tenant_admin, visible only to its members). Documents are live on insert;
-- there is no draft/publish state.
CREATE TABLE knowledge_documents (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id uuid REFERENCES tenants(id) ON DELETE CASCADE,
    title text NOT NULL CHECK (length(btrim(title)) BETWEEN 1 AND 200),
    filename text NOT NULL CHECK (length(filename) BETWEEN 1 AND 255),
    format text NOT NULL CHECK (format IN ('md', 'txt', 'csv', 'docx', 'xlsx', 'pdf')),
    -- 3 MiB upload cap, enforced here as well as in the handler.
    size_bytes bigint NOT NULL CHECK (size_bytes > 0 AND size_bytes <= 3145728),
    sha256 text NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    storage_key text NOT NULL CHECK (storage_key ~ '^[0-9a-f]{64}$'),
    char_count int NOT NULL DEFAULT 0 CHECK (char_count >= 0),
    chunk_count int NOT NULL DEFAULT 0 CHECK (chunk_count >= 0),
    -- Nullable so removing a user keeps the knowledge they contributed.
    uploaded_by uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    -- One copy of identical content per scope. A plain UNIQUE (tenant_id,
    -- sha256) would let unlimited duplicates in the global scope because NULLs
    -- compare as distinct; NULLS NOT DISTINCT (PG15+) treats all NULL
    -- tenant_ids as one scope with a single constraint, instead of two partial
    -- unique indexes that the repository would have to know about separately.
    CONSTRAINT knowledge_documents_scope_sha256_key UNIQUE NULLS NOT DISTINCT (tenant_id, sha256)
);

CREATE INDEX knowledge_documents_tenant_idx ON knowledge_documents(tenant_id, created_at DESC, id DESC);

-- A document never moves between scopes. Re-scoping would orphan the chunk
-- copies of tenant_id below, so it is rejected outright (re-upload instead).
CREATE FUNCTION reject_knowledge_document_rescope() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.tenant_id IS DISTINCT FROM OLD.tenant_id THEN
        RAISE EXCEPTION 'knowledge_documents.tenant_id is immutable';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER knowledge_documents_scope_immutable
    BEFORE UPDATE OF tenant_id ON knowledge_documents
    FOR EACH ROW EXECUTE FUNCTION reject_knowledge_document_rescope();

CREATE TABLE knowledge_chunks (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    document_id uuid NOT NULL REFERENCES knowledge_documents(id) ON DELETE CASCADE,
    -- Copy of the document's tenant_id so RLS and tenant-filtered search need
    -- no join. Kept honest by the trigger below (see there for why not a FK).
    tenant_id uuid,
    position int NOT NULL CHECK (position >= 0),
    heading_path text NOT NULL DEFAULT '' CHECK (length(heading_path) <= 1000),
    locator text NOT NULL DEFAULT '' CHECK (length(locator) <= 200),
    content text NOT NULL CHECK (length(btrim(content)) >= 1 AND length(content) <= 20000),
    -- Indonesian stemming/stop words (the config exists in PG18). Headings
    -- weigh more than body text so a title match ranks first.
    search tsvector GENERATED ALWAYS AS (
        setweight(to_tsvector('indonesian'::regconfig, coalesce(heading_path, '')), 'A') ||
        setweight(to_tsvector('indonesian'::regconfig, coalesce(content, '')), 'B')
    ) STORED,
    CONSTRAINT knowledge_chunks_document_position_key UNIQUE (document_id, position)
);

CREATE INDEX knowledge_chunks_search_idx ON knowledge_chunks USING gin (search);
CREATE INDEX knowledge_chunks_tenant_idx ON knowledge_chunks(tenant_id);

-- Why a trigger and not a composite FK (document_id, tenant_id): a
-- foreign key with a NULL member is not checked under MATCH SIMPLE, and MATCH
-- FULL rejects a (non-null, NULL) pair, so neither can express "global chunk
-- of a global document". The trigger compares with IS DISTINCT FROM, which
-- handles NULL correctly, and fails loudly on mismatch or a missing/invisible
-- document. It runs as the invoker, so it also cannot be used to probe
-- documents the caller cannot see.
CREATE FUNCTION enforce_knowledge_chunk_scope() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    doc_found boolean;
    doc_tenant uuid;
BEGIN
    SELECT true, d.tenant_id INTO doc_found, doc_tenant
    FROM knowledge_documents d
    WHERE d.id = NEW.document_id;

    IF doc_found IS NOT TRUE THEN
        RAISE EXCEPTION 'knowledge_chunks.document_id does not reference a visible document';
    END IF;
    IF NEW.tenant_id IS DISTINCT FROM doc_tenant THEN
        RAISE EXCEPTION 'knowledge_chunks.tenant_id must equal the tenant_id of its document';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER knowledge_chunks_scope
    BEFORE INSERT OR UPDATE OF document_id, tenant_id ON knowledge_chunks
    FOR EACH ROW EXECUTE FUNCTION enforce_knowledge_chunk_scope();

ALTER TABLE knowledge_documents ENABLE ROW LEVEL SECURITY;
ALTER TABLE knowledge_documents FORCE ROW LEVEL SECURITY;
ALTER TABLE knowledge_chunks ENABLE ROW LEVEL SECURITY;
ALTER TABLE knowledge_chunks FORCE ROW LEVEL SECURITY;

-- The rules live in two helpers so documents and chunks cannot drift apart.
-- Both run as the caller (not SECURITY DEFINER), so they see exactly what the
-- runtime role sees and cannot be used to escalate.
--
-- Read: the caller must be a real user (no app.user_id, no rows). A global row
-- (tenant_id NULL) is then visible everywhere, including platform chat where
-- app.tenant_id is empty. A tenant row needs the tenant pinned in the
-- transaction AND a membership of the caller in it.
CREATE FUNCTION knowledge_can_read(row_tenant uuid) RETURNS boolean
LANGUAGE sql STABLE AS $$
    SELECT EXISTS (
        SELECT 1 FROM users u
        WHERE u.id = nullif(current_setting('app.user_id', true), '')::uuid
    )
    AND (
        row_tenant IS NULL
        OR (
            row_tenant = nullif(current_setting('app.tenant_id', true), '')::uuid
            AND EXISTS (
                SELECT 1 FROM tenant_memberships m
                WHERE m.user_id = nullif(current_setting('app.user_id', true), '')::uuid
                  AND m.tenant_id = row_tenant
            )
        )
    )
$$;

-- Write: global rows need users.role = 'super_user' read from the database,
-- never from request input. Tenant rows need the caller to be a tenant_admin
-- (not a reviewer) of that tenant, and the tenant must be the one pinned in
-- the transaction, so a tenant admin cannot write into another tenant even
-- when they administer several.
CREATE FUNCTION knowledge_can_write(row_tenant uuid) RETURNS boolean
LANGUAGE sql STABLE AS $$
    SELECT CASE
        WHEN row_tenant IS NULL THEN EXISTS (
            SELECT 1 FROM users u
            WHERE u.id = nullif(current_setting('app.user_id', true), '')::uuid
              AND u.role = 'super_user'
        )
        ELSE row_tenant = nullif(current_setting('app.tenant_id', true), '')::uuid
            AND EXISTS (
                SELECT 1 FROM tenant_memberships m
                WHERE m.user_id = nullif(current_setting('app.user_id', true), '')::uuid
                  AND m.tenant_id = row_tenant
                  AND m.role = 'tenant_admin'
            )
    END
$$;

CREATE POLICY knowledge_documents_select ON knowledge_documents FOR SELECT
    USING (knowledge_can_read(tenant_id));
CREATE POLICY knowledge_documents_insert ON knowledge_documents FOR INSERT
    WITH CHECK (knowledge_can_write(tenant_id));
CREATE POLICY knowledge_documents_update ON knowledge_documents FOR UPDATE
    USING (knowledge_can_write(tenant_id))
    WITH CHECK (knowledge_can_write(tenant_id));
CREATE POLICY knowledge_documents_delete ON knowledge_documents FOR DELETE
    USING (knowledge_can_write(tenant_id));

-- Chunks follow the identical rules through their own tenant_id copy.
CREATE POLICY knowledge_chunks_select ON knowledge_chunks FOR SELECT
    USING (knowledge_can_read(tenant_id));
CREATE POLICY knowledge_chunks_insert ON knowledge_chunks FOR INSERT
    WITH CHECK (knowledge_can_write(tenant_id));
CREATE POLICY knowledge_chunks_update ON knowledge_chunks FOR UPDATE
    USING (knowledge_can_write(tenant_id))
    WITH CHECK (knowledge_can_write(tenant_id));
CREATE POLICY knowledge_chunks_delete ON knowledge_chunks FOR DELETE
    USING (knowledge_can_write(tenant_id));
