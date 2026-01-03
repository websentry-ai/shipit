-- App secrets - encrypted key-value pairs for sensitive configuration
CREATE TABLE IF NOT EXISTS app_secrets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id UUID NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    key VARCHAR(255) NOT NULL,
    value_encrypted BYTEA NOT NULL,  -- Encrypted using AES-256-GCM
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE(app_id, key)
);

CREATE INDEX idx_app_secrets_app_id ON app_secrets(app_id);
