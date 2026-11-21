-- Fixed bge-m3 dense space. Privileged execution is isolated from both HTTP and worker logins.
CREATE EXTENSION IF NOT EXISTS vector;
DO $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='hsg_embedding_executor') THEN
  CREATE ROLE hsg_embedding_executor NOLOGIN NOSUPERUSER NOBYPASSRLS NOINHERIT;
 END IF;
 IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='hsg_embedding_worker') THEN
  CREATE ROLE hsg_embedding_worker NOLOGIN NOSUPERUSER NOBYPASSRLS NOINHERIT;
 END IF;
END $$;
CREATE TABLE knowledge_embedding_profiles (
 id uuid PRIMARY KEY DEFAULT uuidv7(), model text NOT NULL DEFAULT 'BAAI/bge-m3' CHECK(model='BAAI/bge-m3'),
 revision text NOT NULL CHECK(revision ~ '^[0-9a-f]{40}$'), dimensions integer NOT NULL DEFAULT 1024 CHECK(dimensions=1024),
 normalization text NOT NULL DEFAULT 'l2' CHECK(normalization='l2'), metric text NOT NULL DEFAULT 'cosine' CHECK(metric='cosine'),
 preprocessing text NOT NULL DEFAULT 'kb-chunk-v1' CHECK(preprocessing='kb-chunk-v1'),
 query_prefix text NOT NULL DEFAULT '' CHECK(query_prefix=''), passage_prefix text NOT NULL DEFAULT '' CHECK(passage_prefix=''),
 fingerprint text GENERATED ALWAYS AS (encode(sha256(('BAAI/bge-m3|' || revision || '|1024|l2|cosine|kb-chunk-v1')::bytea),'hex')) STORED UNIQUE,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE knowledge_embedding_config (
 singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton), generation bigint NOT NULL DEFAULT 1, endpoint text NOT NULL DEFAULT '' CHECK(length(endpoint)<=2048),
 profile_id uuid REFERENCES knowledge_embedding_profiles(id), enabled boolean NOT NULL DEFAULT false,
 encrypted_credential bytea, backfill_after uuid, backfill_done boolean NOT NULL DEFAULT false,
 updated_at timestamptz NOT NULL DEFAULT now(), CHECK(NOT enabled OR (profile_id IS NOT NULL AND endpoint<>''))
);
INSERT INTO knowledge_embedding_config(singleton) VALUES(true);
CREATE TABLE knowledge_embedding_jobs (
 id uuid PRIMARY KEY DEFAULT uuidv7(), document_id uuid NOT NULL REFERENCES knowledge_documents(id) ON DELETE CASCADE,
 tenant_id uuid, profile_id uuid NOT NULL REFERENCES knowledge_embedding_profiles(id), config_generation bigint NOT NULL DEFAULT 1, generation bigint NOT NULL DEFAULT 1,
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','running','retry','failed','completed','cancelled')),
 cursor_position integer NOT NULL DEFAULT -1, attempts integer NOT NULL DEFAULT 0 CHECK(attempts BETWEEN 0 AND 5),
 next_attempt_at timestamptz NOT NULL DEFAULT now(), lease_until timestamptz, fence bigint NOT NULL DEFAULT 0,
 error_code text CHECK(error_code IN ('provider_unavailable','provider_invalid','cancelled')), updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(document_id,profile_id)
);
CREATE TABLE knowledge_chunk_embeddings (
 chunk_id uuid NOT NULL REFERENCES knowledge_chunks(id) ON DELETE CASCADE,
 profile_id uuid NOT NULL REFERENCES knowledge_embedding_profiles(id), tenant_id uuid,
 embedding vector(1024) NOT NULL CHECK(abs(vector_norm(embedding)-1)<0.001), content_hash text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(chunk_id,profile_id)
);
CREATE INDEX knowledge_embeddings_scope_profile ON knowledge_chunk_embeddings(tenant_id,profile_id,chunk_id);
-- Small local fixture index. Large production builds must use operator-controlled CONCURRENTLY.
CREATE INDEX knowledge_embeddings_hnsw ON knowledge_chunk_embeddings USING hnsw(embedding vector_cosine_ops);
CREATE INDEX knowledge_embedding_jobs_claim ON knowledge_embedding_jobs(next_attempt_at,lease_until) WHERE state IN ('pending','retry','running');
CREATE FUNCTION knowledge_embedding_scope() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,public AS $$
DECLARE actual uuid; found_row boolean;
BEGIN
 IF TG_TABLE_NAME='knowledge_chunk_embeddings' THEN
  SELECT true,c.tenant_id INTO found_row,actual FROM public.knowledge_chunks c WHERE c.id=NEW.chunk_id;
 ELSE
  SELECT true,d.tenant_id INTO found_row,actual FROM public.knowledge_documents d WHERE d.id=NEW.document_id;
 END IF;
 IF found_row IS NOT TRUE OR actual IS DISTINCT FROM NEW.tenant_id THEN RAISE EXCEPTION 'embedding scope mismatch'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER knowledge_embeddings_scope BEFORE INSERT OR UPDATE ON knowledge_chunk_embeddings FOR EACH ROW EXECUTE FUNCTION knowledge_embedding_scope();
CREATE TRIGGER knowledge_jobs_scope BEFORE INSERT OR UPDATE OF document_id,tenant_id ON knowledge_embedding_jobs FOR EACH ROW EXECUTE FUNCTION knowledge_embedding_scope();
ALTER TABLE knowledge_embedding_profiles ENABLE ROW LEVEL SECURITY;
ALTER TABLE knowledge_embedding_profiles FORCE ROW LEVEL SECURITY;
ALTER TABLE knowledge_embedding_config ENABLE ROW LEVEL SECURITY;
ALTER TABLE knowledge_embedding_config FORCE ROW LEVEL SECURITY;
ALTER TABLE knowledge_embedding_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE knowledge_embedding_jobs FORCE ROW LEVEL SECURITY;
ALTER TABLE knowledge_chunk_embeddings ENABLE ROW LEVEL SECURITY;
ALTER TABLE knowledge_chunk_embeddings FORCE ROW LEVEL SECURITY;
CREATE POLICY embedding_profiles_read ON knowledge_embedding_profiles FOR SELECT USING(knowledge_can_read(NULL));
CREATE POLICY embedding_profiles_write ON knowledge_embedding_profiles FOR INSERT WITH CHECK(knowledge_can_write(NULL));
CREATE POLICY embedding_config_admin ON knowledge_embedding_config USING(knowledge_can_write(NULL)) WITH CHECK(knowledge_can_write(NULL));
CREATE POLICY embedding_jobs_read ON knowledge_embedding_jobs FOR SELECT USING(knowledge_can_read(tenant_id));
CREATE POLICY embedding_jobs_write ON knowledge_embedding_jobs FOR ALL USING(knowledge_can_write(tenant_id)) WITH CHECK(knowledge_can_write(tenant_id));
CREATE POLICY embedding_vectors_read ON knowledge_chunk_embeddings FOR SELECT USING(knowledge_can_read(tenant_id));
CREATE POLICY embedding_executor_profiles ON knowledge_embedding_profiles TO hsg_embedding_executor USING(true) WITH CHECK(true);
CREATE POLICY embedding_executor_config ON knowledge_embedding_config TO hsg_embedding_executor USING(true) WITH CHECK(true);
CREATE POLICY embedding_executor_jobs ON knowledge_embedding_jobs TO hsg_embedding_executor USING(true) WITH CHECK(true);
CREATE POLICY embedding_executor_vectors ON knowledge_chunk_embeddings TO hsg_embedding_executor USING(true) WITH CHECK(true);
CREATE POLICY embedding_executor_documents ON knowledge_documents FOR SELECT TO hsg_embedding_executor USING(true);
CREATE POLICY embedding_executor_document_lock ON knowledge_documents FOR UPDATE TO hsg_embedding_executor USING(true) WITH CHECK(false);
CREATE POLICY embedding_executor_chunks ON knowledge_chunks FOR SELECT TO hsg_embedding_executor USING(true);
GRANT USAGE ON SCHEMA public TO hsg_embedding_executor,hsg_embedding_worker;
GRANT SELECT ON knowledge_documents,knowledge_chunks TO hsg_embedding_executor;
-- Row locks require UPDATE privilege and the lock-only executor policy.
GRANT UPDATE(id) ON knowledge_documents TO hsg_embedding_executor;
GRANT SELECT ON knowledge_embedding_profiles TO hsg_embedding_executor;
GRANT SELECT,UPDATE ON knowledge_embedding_config TO hsg_embedding_executor;
GRANT SELECT,INSERT,UPDATE,DELETE ON knowledge_embedding_jobs,knowledge_chunk_embeddings TO hsg_embedding_executor;
CREATE FUNCTION knowledge_embedding_enqueue() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public AS $$
BEGIN
 INSERT INTO public.knowledge_embedding_jobs(document_id,tenant_id,profile_id,config_generation)
 SELECT d.id,d.tenant_id,c.profile_id,c.generation FROM public.knowledge_documents d CROSS JOIN public.knowledge_embedding_config c
 WHERE d.id=NEW.document_id AND c.enabled AND d.chunk_count>0
 AND (SELECT count(*) FROM public.knowledge_chunks k WHERE k.document_id=d.id)=d.chunk_count
 ON CONFLICT(document_id,profile_id) DO NOTHING;
 RETURN NEW;
END $$;
ALTER FUNCTION knowledge_embedding_enqueue() OWNER TO hsg_embedding_executor;
REVOKE ALL ON FUNCTION knowledge_embedding_enqueue() FROM PUBLIC;
CREATE TRIGGER knowledge_embedding_enqueue AFTER INSERT ON knowledge_chunks FOR EACH ROW EXECUTE FUNCTION knowledge_embedding_enqueue();
-- The worker cannot name a user or tenant, and only receives one leased document.
CREATE FUNCTION knowledge_embedding_claim() RETURNS TABLE(job_id uuid,fence_token bigint,document_id uuid,profile_id uuid,endpoint text,model text,revision text,dimensions integer,encrypted_credential bytea)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public AS $$
DECLARE cfg public.knowledge_embedding_config; j public.knowledge_embedding_jobs; watermark uuid;
BEGIN
 SELECT * INTO cfg FROM public.knowledge_embedding_config WHERE singleton FOR UPDATE;
 UPDATE public.knowledge_embedding_jobs SET state='failed',lease_until=NULL,error_code='provider_unavailable',updated_at=now()
 WHERE state='running' AND lease_until<=now() AND attempts>=5;
 IF NOT cfg.enabled THEN RETURN; END IF;
 IF NOT cfg.backfill_done THEN
  SELECT d.id INTO watermark FROM public.knowledge_documents d WHERE (cfg.backfill_after IS NULL OR d.id>cfg.backfill_after) ORDER BY d.id OFFSET 99 LIMIT 1;
  INSERT INTO public.knowledge_embedding_jobs(document_id,tenant_id,profile_id,config_generation)
  SELECT d.id,d.tenant_id,cfg.profile_id,cfg.generation FROM public.knowledge_documents d
  WHERE (cfg.backfill_after IS NULL OR d.id>cfg.backfill_after) AND (watermark IS NULL OR d.id<=watermark)
   AND d.chunk_count>0 AND d.chunk_count=(SELECT count(*) FROM public.knowledge_chunks k WHERE k.document_id=d.id)
  ORDER BY d.id LIMIT 100 ON CONFLICT ON CONSTRAINT knowledge_embedding_jobs_document_id_profile_id_key DO UPDATE SET
   config_generation=excluded.config_generation,cursor_position=-1,fence=knowledge_embedding_jobs.fence+1,
   state='pending',attempts=0,lease_until=NULL,next_attempt_at=now()
  WHERE knowledge_embedding_jobs.config_generation<>excluded.config_generation;
  UPDATE public.knowledge_embedding_config SET backfill_after=watermark,backfill_done=(watermark IS NULL) WHERE singleton;
 END IF;
 UPDATE public.knowledge_embedding_jobs x SET state='failed',error_code='provider_unavailable',updated_at=now()
 WHERE x.profile_id=cfg.profile_id AND x.attempts>=5 AND x.state IN ('running','retry') AND (x.lease_until IS NULL OR x.lease_until<now());
 SELECT x.* INTO j FROM public.knowledge_embedding_jobs x
 WHERE x.profile_id=cfg.profile_id AND x.config_generation=cfg.generation AND x.attempts<5 AND x.next_attempt_at<=now()
 AND (x.state IN ('pending','retry') OR (x.state='running' AND x.lease_until<now()))
 ORDER BY x.next_attempt_at,x.id FOR UPDATE SKIP LOCKED LIMIT 1;
 IF j.id IS NULL THEN RETURN; END IF;
 UPDATE public.knowledge_embedding_jobs x SET state='running',fence=x.fence+1,attempts=x.attempts+1,lease_until=now()+interval '60 seconds',updated_at=now() WHERE x.id=j.id RETURNING x.* INTO j;
 RETURN QUERY SELECT j.id,j.fence,j.document_id,j.profile_id,cfg.endpoint,p.model,p.revision,p.dimensions,cfg.encrypted_credential FROM public.knowledge_embedding_profiles p WHERE p.id=j.profile_id;
END $$;
CREATE FUNCTION knowledge_embedding_read(job uuid,token bigint) RETURNS TABLE(chunk_id uuid,"position" integer,content_hash text,input text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,public AS $$
 SELECT k.id,k.position,encode(sha256(convert_to(k.heading_path || E'\n' || k.content,'UTF8')),'hex'),k.heading_path || E'\n' || k.content
 FROM public.knowledge_embedding_jobs j JOIN public.knowledge_chunks k ON k.document_id=j.document_id
 JOIN public.knowledge_embedding_config cfg ON cfg.singleton AND cfg.enabled AND cfg.profile_id=j.profile_id AND cfg.generation=j.config_generation
 WHERE j.id=job AND j.fence=token AND j.state='running' AND j.lease_until>now() AND k.position>j.cursor_position
 ORDER BY k.position LIMIT 32
$$;
CREATE FUNCTION knowledge_embedding_commit(job uuid,token bigint,items jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public AS $$
DECLARE j public.knowledge_embedding_jobs; expected integer; supplied integer; last_position integer;
BEGIN
 SELECT x.* INTO j FROM public.knowledge_embedding_jobs x JOIN public.knowledge_embedding_config cfg ON cfg.singleton AND cfg.enabled AND cfg.profile_id=x.profile_id AND cfg.generation=x.config_generation
 WHERE x.id=job AND x.fence=token AND x.state='running' AND x.lease_until>now() FOR UPDATE OF x FOR SHARE OF cfg;
 IF j.id IS NULL THEN RETURN false; END IF;
 PERFORM 1 FROM public.knowledge_documents d WHERE d.id=j.document_id FOR KEY SHARE;
 IF NOT FOUND THEN RETURN false; END IF;
 SELECT count(*) INTO expected FROM public.knowledge_embedding_read(job,token);
 SELECT count(*),max(k.position) INTO supplied,last_position FROM jsonb_to_recordset(items) AS v(chunk_id uuid,content_hash text,embedding text)
 JOIN public.knowledge_embedding_read(job,token) k ON k.chunk_id=v.chunk_id AND k.content_hash=v.content_hash;
 IF jsonb_typeof(items)<>'array' OR jsonb_array_length(items)>32 OR supplied<>expected OR supplied<>jsonb_array_length(items)
 OR (SELECT count(DISTINCT v->>'chunk_id') FROM jsonb_array_elements(items) v)<>supplied THEN RETURN false; END IF;
 INSERT INTO public.knowledge_chunk_embeddings(chunk_id,profile_id,tenant_id,embedding,content_hash)
 SELECT v.chunk_id,j.profile_id,j.tenant_id,v.embedding::public.vector(1024),v.content_hash FROM jsonb_to_recordset(items) AS v(chunk_id uuid,content_hash text,embedding text)
 ON CONFLICT(chunk_id,profile_id) DO UPDATE SET embedding=excluded.embedding,content_hash=excluded.content_hash,created_at=now();
 UPDATE public.knowledge_embedding_jobs x SET cursor_position=coalesce(last_position,x.cursor_position), attempts=0,
 state=CASE WHEN EXISTS(SELECT 1 FROM public.knowledge_chunks k WHERE k.document_id=j.document_id AND k.position>coalesce(last_position,j.cursor_position)) THEN 'pending' ELSE 'completed' END,
 lease_until=NULL,error_code=NULL,updated_at=now() WHERE x.id=j.id;
 RETURN true;
END $$;
CREATE FUNCTION knowledge_embedding_fail(job uuid,token bigint,code text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public AS $$
BEGIN
 IF code NOT IN ('provider_unavailable','provider_invalid') THEN RETURN false; END IF;
 UPDATE public.knowledge_embedding_jobs SET state=CASE WHEN attempts>=5 THEN 'failed' ELSE 'retry' END,
 next_attempt_at=now()+make_interval(secs=>least(300,power(2,attempts)::integer)),lease_until=NULL,error_code=code,updated_at=now()
 WHERE id=job AND fence=token AND state='running' AND lease_until>now();
 RETURN FOUND;
END $$;
CREATE FUNCTION knowledge_embedding_serving() RETURNS uuid LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,public AS $$
 SELECT profile_id FROM public.knowledge_embedding_config WHERE singleton AND enabled
$$;
ALTER FUNCTION knowledge_embedding_claim() OWNER TO hsg_embedding_executor;
ALTER FUNCTION knowledge_embedding_read(uuid,bigint) OWNER TO hsg_embedding_executor;
ALTER FUNCTION knowledge_embedding_commit(uuid,bigint,jsonb) OWNER TO hsg_embedding_executor;
ALTER FUNCTION knowledge_embedding_fail(uuid,bigint,text) OWNER TO hsg_embedding_executor;
ALTER FUNCTION knowledge_embedding_serving() OWNER TO hsg_embedding_executor;
REVOKE ALL ON FUNCTION knowledge_embedding_claim(),knowledge_embedding_read(uuid,bigint),knowledge_embedding_commit(uuid,bigint,jsonb),knowledge_embedding_fail(uuid,bigint,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION knowledge_embedding_claim(),knowledge_embedding_read(uuid,bigint),knowledge_embedding_commit(uuid,bigint,jsonb),knowledge_embedding_fail(uuid,bigint,text) TO hsg_embedding_worker;
-- Runtime HTTP role gets table SELECT/INSERT/UPDATE explicitly from the operator;
-- the worker login only inherits hsg_embedding_worker, never the executor.

-- immutable profile identity, including preprocessing and empty bge-m3 prefixes.
CREATE FUNCTION knowledge_embedding_profile_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'embedding profiles are immutable'; END $$;
CREATE TRIGGER knowledge_embedding_profile_immutable BEFORE UPDATE OR DELETE ON knowledge_embedding_profiles FOR EACH ROW EXECUTE FUNCTION knowledge_embedding_profile_immutable();
CREATE FUNCTION knowledge_embedding_worker_config(job uuid,token bigint) RETURNS TABLE(endpoint text,model text,revision text,dimensions integer,enabled boolean,encrypted_credential bytea,updated_at timestamptz,profile_id text,generation bigint)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,public AS $$
 SELECT c.endpoint,p.model,p.revision,p.dimensions,c.enabled,c.encrypted_credential,c.updated_at,p.id::text,c.generation
 FROM public.knowledge_embedding_jobs j JOIN public.knowledge_embedding_config c ON c.singleton AND c.enabled AND c.profile_id=j.profile_id AND c.generation=j.config_generation
 JOIN public.knowledge_embedding_profiles p ON p.id=c.profile_id
 WHERE j.id=job AND j.fence=token AND j.state='running' AND j.lease_until>now()
$$;
ALTER FUNCTION knowledge_embedding_worker_config(uuid,bigint) OWNER TO hsg_embedding_executor;
REVOKE ALL ON FUNCTION knowledge_embedding_worker_config(uuid,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION knowledge_embedding_worker_config(uuid,bigint) TO hsg_embedding_worker;
GRANT SELECT ON users,tenant_memberships TO hsg_embedding_executor;
CREATE FUNCTION knowledge_embedding_status(doc uuid,scope uuid) RETURNS TABLE(status text,indexed_chunks bigint,profile_revision text,updated_at timestamptz,error_code text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,public AS $$
 SELECT CASE WHEN NOT c.enabled THEN 'not_configured' WHEN j.state='completed' THEN 'ready' WHEN j.state='running' THEN 'indexing' WHEN j.state='failed' THEN 'failed' ELSE 'pending' END,
 (SELECT count(*) FROM public.knowledge_chunk_embeddings e JOIN public.knowledge_chunks k ON k.id=e.chunk_id WHERE k.document_id=d.id AND e.profile_id=c.profile_id),coalesce(p.revision,''),coalesce(j.updated_at,c.updated_at),coalesce(j.error_code,'')
 FROM public.knowledge_documents d CROSS JOIN public.knowledge_embedding_config c
 LEFT JOIN public.knowledge_embedding_profiles p ON p.id=c.profile_id
 LEFT JOIN public.knowledge_embedding_jobs j ON j.document_id=d.id AND j.profile_id=c.profile_id AND j.config_generation=c.generation
 WHERE d.id=doc AND d.tenant_id IS NOT DISTINCT FROM scope AND public.knowledge_can_read(d.tenant_id) AND c.singleton
$$;
CREATE FUNCTION knowledge_embedding_retry(doc uuid,scope uuid) RETURNS void
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,public AS $$
 INSERT INTO public.knowledge_embedding_jobs(document_id,tenant_id,profile_id,config_generation)
 SELECT d.id,d.tenant_id,c.profile_id,c.generation FROM public.knowledge_documents d CROSS JOIN public.knowledge_embedding_config c
 WHERE d.id=doc AND d.tenant_id IS NOT DISTINCT FROM scope AND public.knowledge_can_write(d.tenant_id) AND c.singleton AND c.enabled
 ON CONFLICT ON CONSTRAINT knowledge_embedding_jobs_document_id_profile_id_key DO UPDATE SET state='pending',attempts=0,next_attempt_at=now(),lease_until=NULL,fence=knowledge_embedding_jobs.fence+1,error_code=NULL,config_generation=excluded.config_generation
 WHERE knowledge_embedding_jobs.state IN ('failed','retry','cancelled') OR knowledge_embedding_jobs.config_generation<>excluded.config_generation
$$;
ALTER FUNCTION knowledge_embedding_status(uuid,uuid) OWNER TO hsg_embedding_executor;
ALTER FUNCTION knowledge_embedding_retry(uuid,uuid) OWNER TO hsg_embedding_executor;
-- These two entry points enforce actor/scope themselves and reveal no configuration or secrets.
-- end
