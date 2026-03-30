-- Pre-deploy hooks: run commands before deployment (e.g., database migrations)

-- Add pre_deploy_command to apps
ALTER TABLE apps ADD COLUMN pre_deploy_command TEXT;

-- Add pre_deploy_command to revisions for rollback support
ALTER TABLE app_revisions ADD COLUMN pre_deploy_command TEXT;
