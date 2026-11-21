-- RLS / FTS test for migration 000018_knowledge_base.
--
-- Not embedded in the binary (migrations.go only embeds *.sql in this dir, not
-- testdata/). Run against a THROWAWAY Postgres 18 with migrations 000001..000018
-- already applied, as a superuser/owner. Everything happens in one transaction
-- that is rolled back, so the database is left untouched:
--
--   docker run -d --rm --name kb-test -e POSTGRES_PASSWORD=pw -p 127.0.0.1::5432 postgres:18-alpine
--   docker exec kb-test psql -U postgres -c 'CREATE DATABASE kbtest'
--   for f in server/migrations/*.up.sql; do docker exec -i kb-test psql -v ON_ERROR_STOP=1 -q -U postgres -d kbtest < $f; done
--   docker exec -i kb-test psql -v ON_ERROR_STOP=1 -U postgres -d kbtest < server/migrations/testdata/000018_knowledge_base_rls_test.sql
--
-- Any failed assertion raises an exception (non-zero exit with ON_ERROR_STOP).
\set ON_ERROR_STOP on
BEGIN;

CREATE ROLE kb_test_runtime LOGIN NOSUPERUSER NOBYPASSRLS;
GRANT USAGE ON SCHEMA public TO kb_test_runtime;
GRANT SELECT, INSERT, UPDATE, DELETE ON tenants, tenant_memberships, knowledge_documents, knowledge_chunks TO kb_test_runtime;
GRANT SELECT ON users TO kb_test_runtime;

CREATE FUNCTION kb_t_ctx(uid text, tid text) RETURNS void LANGUAGE sql AS $$
    SELECT set_config('app.user_id', uid, true), set_config('app.tenant_id', tid, true) $$;
CREATE FUNCTION kb_t_assert(ok boolean, label text) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
    IF ok IS NOT TRUE THEN RAISE EXCEPTION 'FAIL: %', label; END IF;
    RAISE NOTICE 'PASS: %', label;
END $$;
-- True when the statement raises any error (RLS denial, trigger, constraint).
CREATE FUNCTION kb_t_denied(stmt text) RETURNS boolean LANGUAGE plpgsql AS $$
BEGIN
    EXECUTE stmt;
    RETURN false;
EXCEPTION WHEN OTHERS THEN
    RETURN true;
END $$;
-- Rows affected by a DML statement.
CREATE FUNCTION kb_t_rows(stmt text) RETURNS bigint LANGUAGE plpgsql AS $$
DECLARE n bigint;
BEGIN
    EXECUTE stmt;
    GET DIAGNOSTICS n = ROW_COUNT;
    RETURN n;
END $$;
GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA public TO kb_test_runtime;

-- Seed as owner/superuser (bypasses RLS).
INSERT INTO users (id, name, email, role) VALUES
 ('00000000-0000-0000-0000-0000000000a1', 'su',      'su@t',      'super_user'),
 ('00000000-0000-0000-0000-0000000000a2', 'plain',   'plain@t',   'user'),
 ('00000000-0000-0000-0000-0000000000a3', 'adminA',  'aa@t',      'user'),
 ('00000000-0000-0000-0000-0000000000a4', 'revA',    'ra@t',      'user'),
 ('00000000-0000-0000-0000-0000000000a5', 'adminB',  'ab@t',      'user'),
 ('00000000-0000-0000-0000-0000000000a6', 'adminAB', 'aab@t',     'user');
INSERT INTO tenants (id, name) VALUES
 ('00000000-0000-0000-0000-00000000000a', 'A'),
 ('00000000-0000-0000-0000-00000000000b', 'B');
INSERT INTO tenant_memberships (tenant_id, user_id, role) VALUES
 ('00000000-0000-0000-0000-00000000000a', '00000000-0000-0000-0000-0000000000a3', 'tenant_admin'),
 ('00000000-0000-0000-0000-00000000000a', '00000000-0000-0000-0000-0000000000a4', 'reviewer'),
 ('00000000-0000-0000-0000-00000000000b', '00000000-0000-0000-0000-0000000000a5', 'tenant_admin'),
 ('00000000-0000-0000-0000-00000000000a', '00000000-0000-0000-0000-0000000000a6', 'tenant_admin'),
 ('00000000-0000-0000-0000-00000000000b', '00000000-0000-0000-0000-0000000000a6', 'tenant_admin');

SET LOCAL ROLE kb_test_runtime;

-- (1) super_user writes global docs; a regular user cannot ---------------------
SELECT kb_t_ctx('00000000-0000-0000-0000-0000000000a1', '');
INSERT INTO knowledge_documents (id, tenant_id, title, filename, format, size_bytes, sha256, storage_key, uploaded_by)
VALUES ('00000000-0000-0000-0000-0000000001a0', NULL, 'Global', 'g.md', 'md', 10, repeat('a',64), repeat('a',64), '00000000-0000-0000-0000-0000000000a1');
INSERT INTO knowledge_chunks (document_id, tenant_id, position, heading_path, content)
VALUES ('00000000-0000-0000-0000-0000000001a0', NULL, 0, 'Pajak > PPN', 'Tarif pajak pertambahan nilai adalah dua belas persen.');
SELECT kb_t_assert(kb_t_denied($$INSERT INTO knowledge_documents (tenant_id,title,filename,format,size_bytes,sha256,storage_key)
  VALUES (NULL,'dup','g.md','md',10,repeat('a',64),repeat('a',64))$$), '1a duplicate global sha256 rejected (NULLS NOT DISTINCT)');
SELECT kb_t_assert(kb_t_denied($$INSERT INTO knowledge_chunks (document_id,tenant_id,position,content)
  VALUES ('00000000-0000-0000-0000-0000000001a0','00000000-0000-0000-0000-00000000000a',1,'x')$$), '1b chunk tenant_id cannot diverge from its document');
SELECT kb_t_assert(kb_t_denied($$UPDATE knowledge_documents SET tenant_id='00000000-0000-0000-0000-00000000000a' WHERE id='00000000-0000-0000-0000-0000000001a0'$$), '1c document cannot be re-scoped');

SELECT kb_t_ctx('00000000-0000-0000-0000-0000000000a2', '');
SELECT kb_t_assert(kb_t_denied($$INSERT INTO knowledge_documents (tenant_id,title,filename,format,size_bytes,sha256,storage_key)
  VALUES (NULL,'x','x.md','md',10,repeat('b',64),repeat('b',64))$$), '1d regular user cannot insert global');
SELECT kb_t_assert(kb_t_rows($$DELETE FROM knowledge_documents WHERE tenant_id IS NULL$$) = 0, '1e regular user cannot delete global');
SELECT kb_t_assert(kb_t_rows($$UPDATE knowledge_documents SET title='hacked' WHERE tenant_id IS NULL$$) = 0, '1f regular user cannot update global');

-- (2) tenant_admin writes only its own pinned tenant; reviewer cannot ----------
SELECT kb_t_ctx('00000000-0000-0000-0000-0000000000a3', '00000000-0000-0000-0000-00000000000a');
INSERT INTO knowledge_documents (id, tenant_id, title, filename, format, size_bytes, sha256, storage_key, uploaded_by)
VALUES ('00000000-0000-0000-0000-0000000002a0', '00000000-0000-0000-0000-00000000000a', 'Doc A', 'a.txt', 'txt', 10, repeat('c',64), repeat('c',64), '00000000-0000-0000-0000-0000000000a3');
INSERT INTO knowledge_chunks (document_id, tenant_id, position, heading_path, content)
VALUES ('00000000-0000-0000-0000-0000000002a0', '00000000-0000-0000-0000-00000000000a', 0, 'Cuti', 'Karyawan berhak atas dua belas hari cuti tahunan.');
SELECT kb_t_assert(kb_t_denied($$INSERT INTO knowledge_documents (tenant_id,title,filename,format,size_bytes,sha256,storage_key)
  VALUES ('00000000-0000-0000-0000-00000000000b','x','x.md','md',10,repeat('d',64),repeat('d',64))$$), '2a admin A cannot insert for tenant B');
SELECT kb_t_assert(kb_t_denied($$INSERT INTO knowledge_documents (tenant_id,title,filename,format,size_bytes,sha256,storage_key)
  VALUES (NULL,'x','x.md','md',10,repeat('d',64),repeat('d',64))$$), '2b tenant admin cannot insert global');
SELECT kb_t_assert(kb_t_rows($$DELETE FROM knowledge_documents WHERE tenant_id IS NULL$$) = 0, '2c tenant admin cannot delete global');

-- Admin of both A and B, but pinned to A: cannot write B.
SELECT kb_t_ctx('00000000-0000-0000-0000-0000000000a6', '00000000-0000-0000-0000-00000000000a');
SELECT kb_t_assert(kb_t_denied($$INSERT INTO knowledge_documents (tenant_id,title,filename,format,size_bytes,sha256,storage_key)
  VALUES ('00000000-0000-0000-0000-00000000000b','x','x.md','md',10,repeat('d',64),repeat('d',64))$$), '2d admin of A+B pinned to A cannot write B');

SELECT kb_t_ctx('00000000-0000-0000-0000-0000000000a4', '00000000-0000-0000-0000-00000000000a');
SELECT kb_t_assert(kb_t_denied($$INSERT INTO knowledge_documents (tenant_id,title,filename,format,size_bytes,sha256,storage_key)
  VALUES ('00000000-0000-0000-0000-00000000000a','x','x.md','md',10,repeat('e',64),repeat('e',64))$$), '2e reviewer cannot insert');
SELECT kb_t_assert(kb_t_rows($$UPDATE knowledge_documents SET title='r' WHERE tenant_id='00000000-0000-0000-0000-00000000000a'$$) = 0, '2f reviewer cannot update');
SELECT kb_t_assert(kb_t_rows($$DELETE FROM knowledge_documents WHERE tenant_id='00000000-0000-0000-0000-00000000000a'$$) = 0, '2g reviewer cannot delete');
SELECT kb_t_assert(kb_t_rows($$DELETE FROM knowledge_chunks WHERE tenant_id='00000000-0000-0000-0000-00000000000a'$$) = 0, '2h reviewer cannot delete chunks');

-- Tenant B doc for visibility tests.
SELECT kb_t_ctx('00000000-0000-0000-0000-0000000000a5', '00000000-0000-0000-0000-00000000000b');
INSERT INTO knowledge_documents (id, tenant_id, title, filename, format, size_bytes, sha256, storage_key)
VALUES ('00000000-0000-0000-0000-0000000003b0', '00000000-0000-0000-0000-00000000000b', 'Doc B', 'b.txt', 'txt', 10, repeat('f',64), repeat('f',64));
INSERT INTO knowledge_chunks (document_id, tenant_id, position, content)
VALUES ('00000000-0000-0000-0000-0000000003b0', '00000000-0000-0000-0000-00000000000b', 0, 'Rahasia tenant B tentang cuti.');

-- (3) member of A sees A + global, not B -------------------------------------
SELECT kb_t_ctx('00000000-0000-0000-0000-0000000000a4', '00000000-0000-0000-0000-00000000000a');
SELECT kb_t_assert((SELECT array_agg(title ORDER BY title) FROM knowledge_documents) = ARRAY['Doc A','Global'], '3a member A sees A + global only');
SELECT kb_t_assert((SELECT count(*) FROM knowledge_chunks) = 2, '3b member A sees only 2 chunks (A + global)');
-- Member of A claiming tenant B in the session still sees nothing of B.
SELECT kb_t_ctx('00000000-0000-0000-0000-0000000000a4', '00000000-0000-0000-0000-00000000000b');
SELECT kb_t_assert((SELECT array_agg(title) FROM knowledge_documents) = ARRAY['Global'], '3c non-member pinning tenant B sees global only');

-- (4) platform context (empty tenant) sees global only ------------------------
SELECT kb_t_ctx('00000000-0000-0000-0000-0000000000a3', '');
SELECT kb_t_assert((SELECT array_agg(title) FROM knowledge_documents) = ARRAY['Global'], '4a platform context sees global only');
SELECT kb_t_assert((SELECT count(*) FROM knowledge_chunks) = 1, '4b platform context sees 1 global chunk');

-- (5) no user / unknown user sees nothing ------------------------------------
SELECT kb_t_ctx('', '');
SELECT kb_t_assert((SELECT count(*) FROM knowledge_documents) = 0 AND (SELECT count(*) FROM knowledge_chunks) = 0, '5a empty app.user_id sees nothing');
SELECT kb_t_ctx('00000000-0000-0000-0000-0000000000ff', '00000000-0000-0000-0000-00000000000a');
SELECT kb_t_assert((SELECT count(*) FROM knowledge_documents) = 0, '5b unknown user sees nothing');

-- (6) full-text search --------------------------------------------------------
SELECT kb_t_ctx('00000000-0000-0000-0000-0000000000a3', '00000000-0000-0000-0000-00000000000a');
SELECT kb_t_assert((SELECT array_agg(content) FROM knowledge_chunks WHERE search @@ websearch_to_tsquery('indonesian', 'cuti tahunan')) = ARRAY['Karyawan berhak atas dua belas hari cuti tahunan.'], '6a FTS finds the leave chunk');
SELECT kb_t_assert((SELECT count(*) FROM knowledge_chunks WHERE search @@ websearch_to_tsquery('indonesian', 'pajak')) = 1, '6b FTS matches global chunk via heading (weight A)');
SELECT kb_t_assert((SELECT count(*) FROM knowledge_chunks WHERE search @@ websearch_to_tsquery('indonesian', 'rahasia')) = 0, '6c FTS never returns tenant B chunk to tenant A');
SELECT kb_t_assert((SELECT count(*) FROM knowledge_chunks WHERE search @@ websearch_to_tsquery('indonesian', 'memperhitungkan -pajak')) = 0, '6d websearch negation works');

-- (1 cont.) super_user can delete global; cascade removes its chunks ---------
RESET ROLE; SET LOCAL ROLE kb_test_runtime;
SELECT kb_t_ctx('00000000-0000-0000-0000-0000000000a1', '');
SELECT kb_t_assert(kb_t_rows($$DELETE FROM knowledge_documents WHERE id='00000000-0000-0000-0000-0000000001a0'$$) = 1, '1g super_user can delete global doc');
RESET ROLE;
SELECT kb_t_assert((SELECT count(*) FROM knowledge_chunks WHERE document_id='00000000-0000-0000-0000-0000000001a0') = 0, '1h deleting document cascades to chunks');

ROLLBACK;
