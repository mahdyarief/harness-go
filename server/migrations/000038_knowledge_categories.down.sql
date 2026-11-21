DROP TRIGGER IF EXISTS knowledge_documents_category_scope ON knowledge_documents;
DROP FUNCTION IF EXISTS enforce_knowledge_document_category_scope();
DROP INDEX IF EXISTS knowledge_documents_category_idx;
ALTER TABLE knowledge_documents DROP COLUMN IF EXISTS category_id;

DROP POLICY IF EXISTS knowledge_categories_delete ON knowledge_categories;
DROP POLICY IF EXISTS knowledge_categories_update ON knowledge_categories;
DROP POLICY IF EXISTS knowledge_categories_insert ON knowledge_categories;
DROP POLICY IF EXISTS knowledge_categories_select ON knowledge_categories;
DROP TRIGGER IF EXISTS knowledge_categories_scope_immutable ON knowledge_categories;
DROP FUNCTION IF EXISTS reject_knowledge_category_rescope();
DROP TABLE IF EXISTS knowledge_categories;
