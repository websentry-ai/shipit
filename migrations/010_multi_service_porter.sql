-- Phase 3: Multi-service support and Porter integration
-- This migration adds support for Porter-style multi-service apps

-- Add service grouping and management tracking columns
ALTER TABLE apps ADD COLUMN service_name VARCHAR(255);
ALTER TABLE apps ADD COLUMN app_group VARCHAR(255);
ALTER TABLE apps ADD COLUMN managed_by VARCHAR(50) DEFAULT 'shipit'; -- shipit, porter, or null for discovered apps

-- Add same columns to app_revisions for historical tracking
ALTER TABLE app_revisions ADD COLUMN service_name VARCHAR(255);
ALTER TABLE app_revisions ADD COLUMN app_group VARCHAR(255);
ALTER TABLE app_revisions ADD COLUMN managed_by VARCHAR(50);

-- For existing shipit-managed apps, set service_name = name (single-service pattern)
UPDATE apps SET
    service_name = name,
    app_group = name,
    managed_by = 'shipit'
WHERE managed_by IS NULL;

-- For existing revisions, copy from apps table
UPDATE app_revisions ar
SET
    service_name = a.service_name,
    app_group = a.app_group,
    managed_by = a.managed_by
FROM apps a
WHERE ar.app_id = a.id;

-- Create index for efficient app group queries
CREATE INDEX idx_apps_app_group ON apps(app_group);
CREATE INDEX idx_apps_managed_by ON apps(managed_by);

-- Update unique constraint to allow multiple services per app group
-- Drop old constraint
ALTER TABLE apps DROP CONSTRAINT apps_cluster_id_namespace_name_key;

-- Add new constraint: unique deployment name per cluster/namespace
-- (allows same app_group with different service_names)
ALTER TABLE apps ADD CONSTRAINT apps_cluster_namespace_name_unique
    UNIQUE(cluster_id, namespace, name);

-- Add comment explaining the schema
COMMENT ON COLUMN apps.name IS 'Kubernetes deployment name (e.g., staging-gateway-gateway)';
COMMENT ON COLUMN apps.service_name IS 'Service name within the app (e.g., gateway, gateway-data, web)';
COMMENT ON COLUMN apps.app_group IS 'Logical app grouping, usually the repo/Porter app name (e.g., staging-gateway)';
COMMENT ON COLUMN apps.managed_by IS 'Management system: shipit (deployed via shipit), porter (imported from Porter), null (discovered but unmanaged)';
