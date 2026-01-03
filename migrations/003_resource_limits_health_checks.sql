-- Add resource limits and health check configuration to apps table

-- Resource limits (CPU and memory)
ALTER TABLE apps ADD COLUMN IF NOT EXISTS cpu_request VARCHAR(50) DEFAULT '100m';
ALTER TABLE apps ADD COLUMN IF NOT EXISTS cpu_limit VARCHAR(50) DEFAULT '500m';
ALTER TABLE apps ADD COLUMN IF NOT EXISTS memory_request VARCHAR(50) DEFAULT '128Mi';
ALTER TABLE apps ADD COLUMN IF NOT EXISTS memory_limit VARCHAR(50) DEFAULT '256Mi';

-- Health check configuration
ALTER TABLE apps ADD COLUMN IF NOT EXISTS health_path VARCHAR(255);
ALTER TABLE apps ADD COLUMN IF NOT EXISTS health_port INTEGER;
ALTER TABLE apps ADD COLUMN IF NOT EXISTS health_initial_delay INTEGER DEFAULT 10;
ALTER TABLE apps ADD COLUMN IF NOT EXISTS health_period INTEGER DEFAULT 30;
