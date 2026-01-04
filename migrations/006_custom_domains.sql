-- Add custom domain support for apps

-- Domain field on apps table
ALTER TABLE apps ADD COLUMN IF NOT EXISTS domain VARCHAR(255);
ALTER TABLE apps ADD COLUMN IF NOT EXISTS domain_status VARCHAR(50) DEFAULT 'pending';

-- Domain snapshot on app_revisions table
ALTER TABLE app_revisions ADD COLUMN IF NOT EXISTS domain VARCHAR(255);

-- Index for domain lookups
CREATE INDEX IF NOT EXISTS idx_apps_domain ON apps(domain) WHERE domain IS NOT NULL;
