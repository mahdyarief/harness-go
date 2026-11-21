-- Knowledge categories: optional master data used to group documents inside
-- one library. A category carries the same tenant_id semantics as the document
-- it groups: NULL is the global library (managed by a super_user), a value is
-- that tenant's library. A document may have no category at all, so the
-- category is a facet, never a requirement.
CREATE TABLE knowledge_categories (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id uuid REFERENCES tenants(id) ON DELETE CASCADE,
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 80),
    -- Case/whitespace-folded name, so "Klaim BPJS" and "klaim  bpjs" collide.
    slug text NOT NULL CHECK (length(slug) BETWEEN 1 AND 80),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    -- NULLS NOT DISTINCT makes every global category share one scope instead
    -- of being unconstrained, mirroring knowledge_documents_scope_sha256_key.
    CONSTRAINT knowledge_categories_scope_slug_key UNIQUE NULLS NOT DISTINCT (tenant_id, slug)
);

CREATE INDEX knowledge_categories_tenant_idx ON knowledge_categories(tenant_id, name, id);

-- A category never moves between scopes: the documents pointing at it live in
-- one library, and re-scoping would silently regroup them.
CREATE FUNCTION reject_knowledge_category_rescope() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.tenant_id IS DISTINCT FROM OLD.tenant_id THEN
        RAISE EXCEPTION 'knowledge_categories.tenant_id is immutable';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER knowledge_categories_scope_immutable
    BEFORE UPDATE OF tenant_id ON knowledge_categories
    FOR EACH ROW EXECUTE FUNCTION reject_knowledge_category_rescope();

ALTER TABLE knowledge_categories ENABLE ROW LEVEL SECURITY;
ALTER TABLE knowledge_categories FORCE ROW LEVEL SECURITY;

-- Same two helpers as documents: read follows the library visibility, write
-- needs super_user for global rows or tenant_admin for tenant rows.
CREATE POLICY knowledge_categories_select ON knowledge_categories FOR SELECT
    USING (knowledge_can_read(tenant_id));
CREATE POLICY knowledge_categories_insert ON knowledge_categories FOR INSERT
    WITH CHECK (knowledge_can_write(tenant_id));
CREATE POLICY knowledge_categories_update ON knowledge_categories FOR UPDATE
    USING (knowledge_can_write(tenant_id))
    WITH CHECK (knowledge_can_write(tenant_id));
CREATE POLICY knowledge_categories_delete ON knowledge_categories FOR DELETE
    USING (knowledge_can_write(tenant_id));

-- ON DELETE SET NULL: removing a category never removes or blocks documents,
-- it only returns them to "Tanpa kategori".
ALTER TABLE knowledge_documents
    ADD COLUMN category_id uuid REFERENCES knowledge_categories(id) ON DELETE SET NULL;

CREATE INDEX knowledge_documents_category_idx ON knowledge_documents(category_id);

-- A document may only reference a category from its own library. Without this
-- a tenant document could point at a global category and a later category
-- delete would touch a scope the deleter never had rights over.
CREATE FUNCTION enforce_knowledge_document_category_scope() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.category_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM knowledge_categories c
        WHERE c.id = NEW.category_id
          AND c.tenant_id IS NOT DISTINCT FROM NEW.tenant_id
    ) THEN
        RAISE EXCEPTION 'knowledge_documents.category_id must belong to the document scope';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER knowledge_documents_category_scope
    BEFORE INSERT OR UPDATE OF category_id, tenant_id ON knowledge_documents
    FOR EACH ROW EXECUTE FUNCTION enforce_knowledge_document_category_scope();
