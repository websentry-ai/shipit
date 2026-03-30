-- Deployment history tracking

-- Add deployment status to revisions
ALTER TABLE app_revisions ADD COLUMN deploy_status VARCHAR(50) DEFAULT 'success';
ALTER TABLE app_revisions ADD COLUMN deploy_message TEXT;
ALTER TABLE app_revisions ADD COLUMN deployed_at TIMESTAMP DEFAULT NOW();
