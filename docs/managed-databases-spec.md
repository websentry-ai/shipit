# Managed Databases (RDS) — Design Spec

Status: pre-implementation, ready for a focused agent to scope phase-1.

## Goal

Per-app "Enable Database" toggle in shipit that provisions an AWS RDS Postgres
instance for the service and wires up access via IAM auth (no passwords). Auto-
injects connection env vars on the next deploy.

## Architecture

```
┌────────────────────┐                 ┌─────────────────────────┐
│  shipit (control)  │                 │  AWS account            │
│                    │  CreateDB,...   │                         │
│  internal/aws/rds  ├────────────────►│  RDS Postgres instance  │
│  internal/aws/iam  │                 │  (in cluster's VPC)     │
│  IRSA provisioner  │                 │                         │
└────────┬───────────┘                 │  IAM role per app       │
         │ render                      │  (rds-db:connect scoped │
         │ k8s manifests               │   to one DB user)       │
         ▼                             └────────────┬────────────┘
┌────────────────────┐                              │
│  EKS cluster       │                              │
│                    │  pod's SA → IRSA → IAM role  │
│  ServiceAccount ◄──┼──────────────────────────────┘
│   ↓ (annotated)    │
│  App pod           │  BuildAuthToken() → 15-min token, used as
│   - DB_HOST env    │  Postgres password. TLS required.
│   - AWS_REGION env │
└────────────────────┘
```

## Why IAM auth, not passwords

- **No secret to store, rotate, or leak.** Pod identity is the IAM role.
- **Tokens are 15-min-lived** and generated on demand by the AWS SDK in-process.
- **Fewer moving parts** than Secrets Manager + rotation Lambda + reload sidecar.
- Customer code change: ~5 lines using `aws-sdk-go-v2/feature/rds/auth.BuildAuthToken`
  (or equivalent in `pg` for Node, `psycopg2` + `boto3` for Python).
- Optional RDS Proxy sidecar gets us **zero customer code change** at ~$0.015/hr.

## Components to build

| Piece | Approx LOC | Time |
|---|---|---|
| `internal/aws/rds.go` — Create/Modify/Delete + state machine (10–15min provisioning) | ~600 | 3–4d |
| `internal/aws/iam.go` — IRSA: per-app role, `rds-db:connect` policy, OIDC trust | ~250 | 2d |
| DB-init Job — runs `CREATE USER ... GRANT rds_iam` once on first provision | ~150 | 1d |
| Migration `013_managed_databases.sql` + queries | — | 0.5d |
| API: `POST /apps/:id/database`, `GET`, `DELETE` (snapshot-on-delete) | ~200 | 1d |
| Frontend: "Database" tab — enable button, instance class picker, status | ~400 | 2–3d |
| Renderer wiring: env injection + ServiceAccount annotation | ~100 | 1d |

**Total: ~10–12 working days for a Postgres-only MVP.**

## Open design decisions (resolve first)

1. **Whose AWS account?** Phase 1: same account as the EKS cluster (single-tenant).
   Phase 2: customer-account assume-role.
2. **VPC reachability.** RDS must be in the same VPC as the cluster. Today
   `clusters` table only has `endpoint`. **Schema change**: add `vpc_id`,
   `private_subnet_ids`, `pod_cidr` to `clusters`. Backfill via AWS APIs from
   the cluster ARN.
3. **App code contract.** Two paths:
   - **(A) Direct IAM auth** — customers add 5 lines using `BuildAuthToken()`.
     Cleanest, no extra infra.
   - **(B) RDS Proxy** — sidecar/Service handles IAM, app uses static password
     against proxy. Zero code change, ~$0.015/hr per app, +1 hop latency.
   Recommendation: ship A first, add B as opt-in for static-password apps.
4. **Cost guardrails.**
   - Default + cap at `db.t4g.micro` (~$15/mo) unless cluster has elevated quota.
   - 7-day automated backups, single-AZ.
   - Optional auto-stop after N idle days.
5. **Lifecycle on `DELETE app`.**
   - Default: take final snapshot, then delete instance.
   - Snapshots retained 30 days, then dropped.
   - User-visible "Restore from snapshot" path: out of scope for v1.

## Schema sketch

```sql
-- clusters: add VPC info needed for RDS reachability
ALTER TABLE clusters ADD COLUMN vpc_id VARCHAR(64);
ALTER TABLE clusters ADD COLUMN private_subnet_ids JSONB;
ALTER TABLE clusters ADD COLUMN pod_cidr VARCHAR(32);

-- new table tracking per-app managed databases
CREATE TABLE managed_databases (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  app_id          UUID NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
  engine          VARCHAR(32) NOT NULL DEFAULT 'postgres',
  engine_version  VARCHAR(16) NOT NULL,
  instance_class  VARCHAR(32) NOT NULL DEFAULT 'db.t4g.micro',
  storage_gb      INTEGER NOT NULL DEFAULT 20,
  status          VARCHAR(32) NOT NULL,  -- provisioning|available|modifying|deleting|failed
  status_message  TEXT,
  rds_instance_arn VARCHAR(256),
  rds_endpoint    VARCHAR(256),
  rds_port        INTEGER,
  db_name         VARCHAR(64),
  db_user         VARCHAR(64),
  iam_role_arn    VARCHAR(256),
  service_account VARCHAR(64),
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (app_id)  -- one managed db per app for v1
);
```

## API contract sketch

```
POST   /api/apps/:id/database
       { "instance_class": "db.t4g.micro", "storage_gb": 20, "engine_version": "16" }
       → 202 with status=provisioning

GET    /api/apps/:id/database
       → { status, endpoint, db_name, db_user, env_keys: ["DB_HOST", ...] }

DELETE /api/apps/:id/database?snapshot=true
       → 202 with status=deleting
```

## Env vars injected into the app pod

| Key | Source |
|---|---|
| `DB_HOST` | RDS endpoint |
| `DB_PORT` | 5432 |
| `DB_NAME` | shipit-generated |
| `DB_USER` | shipit-generated, IAM-mapped |
| `DB_SSLMODE` | `require` (RDS forces SSL) |
| `AWS_REGION` | cluster region |

The app's pod ServiceAccount is annotated `eks.amazonaws.com/role-arn=<role>`,
so the AWS SDK auto-credentials. App code uses `BuildAuthToken(host, region, user)`
as the password value.

## Out of scope for v1

- Engines other than Postgres (MySQL/MariaDB/Aurora are easy follow-ups).
- Read replicas.
- Multi-AZ HA toggle (default single-AZ; surface later).
- Cross-region snapshots / DR.
- RDS Proxy provisioning (path B above).
- Per-app DB metrics on the monitoring tab (depends on monitoring branch).
- Customer-account assume-role.
- Backup/restore UI (snapshots happen automatically; user-facing restore is v2).

## Dependencies on other in-flight work

- **`feat/monitoring-historical-graphs`** (active branch) — independent; both can
  proceed in parallel without merge conflicts. Monitoring tab can later show
  RDS CPU/connections/IOPS via CloudWatch metrics.
- **shipit's own Postgres → RDS migration** — separate concern. Same RDS
  provisioning code can be reused, but that's a control-plane change with
  different priorities (backups, HA) and shouldn't be coupled.

## Recommended starting point for the agent

1. Read this spec end-to-end.
2. Make calls on open decisions 1–5; document them in a follow-up commit.
3. Land the schema migration + IAM/IRSA scaffolding first — those have zero
   user-visible surface and unblock everything else.
4. RDS provisioning (state machine + status polling) next.
5. Frontend + renderer wiring last.
