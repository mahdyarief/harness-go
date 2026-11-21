DROP TRIGGER IF EXISTS audit_logs_immutable ON audit_logs;
DROP FUNCTION IF EXISTS reject_audit_log_mutation();
DROP POLICY IF EXISTS audit_log_insert ON audit_logs;
DROP POLICY IF EXISTS audit_log_select ON audit_logs;
DROP TABLE IF EXISTS audit_logs;
