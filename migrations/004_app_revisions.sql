-- App Revisions table for tracking deployment history and enabling rollbacks
-- Each revision stores a snapshot of the app configuration at deploy time

CREATE TABLE IF NOT EXISTS app_revisions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id UUID NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    revision_number INTEGER NOT NULL,

    -- Snapshot of app configuration at deploy time
    image VARCHAR(512) NOT NULL,
    replicas INTEGER NOT NULL DEFAULT 1,
    port INTEGER,
    env_vars JSONB,

    -- Resource limits snapshot
    cpu_request VARCHAR(50),
    cpu_limit VARCHAR(50),
    memory_request VARCHAR(50),
    memory_limit VARCHAR(50),

    -- Health check snapshot
    health_path VARCHAR(255),
    health_port INTEGER,
    health_initial_delay INTEGER,
    health_period INTEGER,

    -- Metadata
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    deployed_by VARCHAR(255),  -- For future: track who deployed

    -- Ensure unique revision numbers per app
    UNIQUE(app_id, revision_number)
);

-- Index for efficient revision lookups
CREATE INDEX IF NOT EXISTS idx_app_revisions_app_id ON app_revisions(app_id);
CREATE INDEX IF NOT EXISTS idx_app_revisions_app_revision ON app_revisions(app_id, revision_number DESC);

-- Add current_revision column to apps table to track active revision
ALTER TABLE apps ADD COLUMN IF NOT EXISTS current_revision INTEGER DEFAULT 0;
