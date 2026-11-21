CREATE TABLE llm_config (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    endpoint TEXT NOT NULL DEFAULT '' CHECK (length(endpoint) <= 2048),
    model TEXT NOT NULL DEFAULT '' CHECK (length(model) <= 255),
    timeout_seconds INTEGER NOT NULL DEFAULT 30 CHECK (timeout_seconds BETWEEN 1 AND 300),
    max_tokens INTEGER NOT NULL DEFAULT 1024 CHECK (max_tokens BETWEEN 1 AND 131072),
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    encrypted_credential BYTEA,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (encrypted_credential IS NULL OR octet_length(encrypted_credential) > 0),
    CHECK (NOT enabled OR (length(btrim(endpoint)) > 0 AND length(btrim(model)) > 0 AND encrypted_credential IS NOT NULL))
);

INSERT INTO llm_config (singleton) VALUES (TRUE);

CREATE TABLE llm_config_audit_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor TEXT NOT NULL CHECK (length(btrim(actor)) BETWEEN 1 AND 255),
    action TEXT NOT NULL CHECK (action IN ('save', 'test_started', 'test_finished')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
