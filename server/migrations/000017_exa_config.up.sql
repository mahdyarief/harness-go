CREATE TABLE exa_config (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    encrypted_keys BYTEA,
    key_count INTEGER NOT NULL DEFAULT 0 CHECK (key_count BETWEEN 0 AND 20),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (encrypted_keys IS NULL OR octet_length(encrypted_keys) > 0),
    CHECK ((encrypted_keys IS NULL) = (key_count = 0))
);

INSERT INTO exa_config (singleton) VALUES (TRUE);

CREATE TABLE exa_config_audit_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor TEXT NOT NULL CHECK (length(btrim(actor)) BETWEEN 1 AND 255),
    action TEXT NOT NULL CHECK (action IN ('save', 'test_started', 'test_finished')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
