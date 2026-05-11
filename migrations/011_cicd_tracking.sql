-- CI/CD tracking: commit SHA pinning, idempotency, and branch tracking groundwork (Phase 5)
ALTER TABLE apps ADD COLUMN last_deployed_sha TEXT;
ALTER TABLE apps ADD COLUMN repo_url TEXT;
ALTER TABLE apps ADD COLUMN tracked_branch TEXT;
ALTER TABLE apps ADD COLUMN deploy_on_push BOOLEAN NOT NULL DEFAULT false;

-- Snapshot the deployed SHA per revision so rollback restores an exact known digest
ALTER TABLE app_revisions ADD COLUMN deployed_sha TEXT;
