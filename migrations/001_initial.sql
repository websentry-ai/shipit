-- API tokens for authentication
CREATE TABLE api_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    token_hash VARCHAR(64) NOT NULL UNIQUE,  -- SHA-256 hash of token
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    last_used_at TIMESTAMP WITH TIME ZONE
);

-- Projects group clusters and apps
CREATE TABLE projects (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL UNIQUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Connected Kubernetes clusters
CREATE TABLE clusters (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    endpoint VARCHAR(512),
    kubeconfig_encrypted BYTEA NOT NULL,  -- Encrypted kubeconfig
    status VARCHAR(50) DEFAULT 'pending', -- pending, connected, error
    status_message TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE(project_id, name)
);

-- Deployed applications
CREATE TABLE apps (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    cluster_id UUID NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    namespace VARCHAR(255) DEFAULT 'default',
    image VARCHAR(512) NOT NULL,
    replicas INTEGER DEFAULT 1,
    port INTEGER,
    env_vars JSONB DEFAULT '{}',
    status VARCHAR(50) DEFAULT 'pending', -- pending, deploying, running, failed
    status_message TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE(cluster_id, namespace, name)
);

-- Indexes
CREATE INDEX idx_clusters_project_id ON clusters(project_id);
CREATE INDEX idx_apps_cluster_id ON apps(cluster_id);
CREATE INDEX idx_api_tokens_hash ON api_tokens(token_hash);
