-- Phase 2.11: per-app zero-downtime mode toggle + advanced overrides.
--
-- Default TRUE matches the current renderer behavior (PDB + topology spread +
-- preStop + readiness probe are already applied unconditionally as of the
-- 2.11 deploy-renderer work). Existing apps keep working as-is; the toggle
-- only changes behavior when an operator explicitly disables it.
--
-- Override columns store user input as either an integer string ("1") or a
-- percentage ("25%"). NULL means "use the derived value from rollingUpdateBudget".
-- Validation lives in the API layer, not the schema.

ALTER TABLE apps ADD COLUMN IF NOT EXISTS zero_downtime_enabled BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE apps ADD COLUMN IF NOT EXISTS max_surge_override VARCHAR(8);
ALTER TABLE apps ADD COLUMN IF NOT EXISTS max_unavailable_override VARCHAR(8);
ALTER TABLE apps ADD COLUMN IF NOT EXISTS max_request_duration_seconds INTEGER NOT NULL DEFAULT 30;

ALTER TABLE app_revisions ADD COLUMN IF NOT EXISTS zero_downtime_enabled BOOLEAN;
ALTER TABLE app_revisions ADD COLUMN IF NOT EXISTS max_surge_override VARCHAR(8);
ALTER TABLE app_revisions ADD COLUMN IF NOT EXISTS max_unavailable_override VARCHAR(8);
ALTER TABLE app_revisions ADD COLUMN IF NOT EXISTS max_request_duration_seconds INTEGER;
