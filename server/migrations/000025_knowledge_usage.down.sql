DROP POLICY IF EXISTS knowledge_document_usage_log_insert ON knowledge_document_usage_log;
DROP POLICY IF EXISTS knowledge_document_usage_log_select ON knowledge_document_usage_log;
DROP TABLE IF EXISTS knowledge_document_usage_log;
DROP FUNCTION IF EXISTS knowledge_can_log_usage(uuid);
