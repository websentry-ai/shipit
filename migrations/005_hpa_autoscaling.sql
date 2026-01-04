-- Add HPA (Horizontal Pod Autoscaler) configuration to apps and app_revisions tables

-- HPA configuration on apps table
ALTER TABLE apps ADD COLUMN IF NOT EXISTS hpa_enabled BOOLEAN DEFAULT FALSE;
ALTER TABLE apps ADD COLUMN IF NOT EXISTS min_replicas INTEGER;
ALTER TABLE apps ADD COLUMN IF NOT EXISTS max_replicas INTEGER;
ALTER TABLE apps ADD COLUMN IF NOT EXISTS cpu_target INTEGER;      -- Target CPU utilization percentage
ALTER TABLE apps ADD COLUMN IF NOT EXISTS memory_target INTEGER;   -- Target memory utilization percentage

-- HPA configuration snapshot on app_revisions table
ALTER TABLE app_revisions ADD COLUMN IF NOT EXISTS hpa_enabled BOOLEAN DEFAULT FALSE;
ALTER TABLE app_revisions ADD COLUMN IF NOT EXISTS min_replicas INTEGER;
ALTER TABLE app_revisions ADD COLUMN IF NOT EXISTS max_replicas INTEGER;
ALTER TABLE app_revisions ADD COLUMN IF NOT EXISTS cpu_target INTEGER;
ALTER TABLE app_revisions ADD COLUMN IF NOT EXISTS memory_target INTEGER;
