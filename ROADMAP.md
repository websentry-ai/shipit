# Shipit Roadmap

This document tracks planned features, development phases, implementation details, and architecture patterns for shipit.

---

## Current Status Summary

**Version:** v0.9.1
**Last Updated:** January 4, 2026
**Production URL:** https://shipit.unboundsec.dev

| Phase | Status | Completion |
|-------|--------|------------|
| Phase 1: Core Platform | ✅ Complete | 100% |
| Phase 2: Production Readiness | ✅ Complete | 100% |
| Phase 2.5: Custom Domains | ✅ Complete | 100% |
| Phase 2.6: Monitoring | ✅ Complete | 100% |
| Phase 2.7: Pre-deploy Hooks | ✅ Complete | 100% |
| Phase 2.8: Google SSO | ✅ Complete | 100% |
| Phase 2.9: Default App URLs | ✅ Complete | 100% |
| Phase 2.10: Design System & Dark Mode | 🟡 In Progress | 0% |
| Phase 2.11: Zero-Downtime Deployment Defaults | 🔴 Planning (P0) | 0% |
| Phase 3: Porter Migration | 🟡 Planning | 0% |
| Phase 4: Observability & Alerts | 🟡 Planning | 0% |
| Phase 5: Branch Tracking & CI/CD Integration | 🔴 Planning (P0) | 0% |
| Phase 6: Advanced Features | 🟡 Planning | 0% |

---

## Completed (v0.1.0 - v0.5.0)

### Core Platform
- [x] Core API server with project/cluster/app management
- [x] CLI client with full CRUD operations
- [x] Multi-cluster support (connect existing Kubernetes clusters)
- [x] Container image deployments
- [x] Log streaming (SSE-based)
- [x] Encrypted kubeconfig storage (AES-256-GCM)
- [x] API token authentication
- [x] External access with TLS (shipit.unboundsec.dev)
- [x] GitHub releases with multi-platform binaries

### Production Features (v0.2.0 - v0.4.0)
- [x] Secrets management (encrypted at rest, injected as K8s Secrets)
- [x] Health checks (liveness/readiness probes)
- [x] Resource limits (CPU/memory requests and limits)
- [x] App revisions (configuration snapshots on deploy)
- [x] Rollbacks (revert to previous revision)

### Web Dashboard (v0.5.0)
- [x] React + TypeScript + TanStack Query SPA
- [x] Project/Cluster/App navigation
- [x] Environment variables CRUD with inline editing
- [x] App details with tabs (Overview, Environment, Secrets, Resources, Health)
- [x] 15 production apps connected from EKS

---

## Infrastructure Details

### Current Stack

```
┌─────────────────────────────────────────────────────────────┐
│                      PRODUCTION                              │
├─────────────────────────────────────────────────────────────┤
│  AWS EKS: unboundsecurity-prod (us-west-2)                  │
│  AWS ECR: 228304386839.dkr.ecr.us-west-2.amazonaws.com      │
│  AWS RDS: shipit-db.c58c2mmu6w5m.us-west-2.rds.amazonaws.com│
│  Domain:  shipit.unboundsec.dev (TLS via AWS ALB)           │
└─────────────────────────────────────────────────────────────┘
```

### Key Files

| File | Purpose |
|------|---------|
| `internal/api/router.go` | Chi router with all API routes |
| `internal/api/handlers.go` | HTTP handlers for all endpoints |
| `internal/db/db.go` | PostgreSQL database layer |
| `internal/k8s/client.go` | Kubernetes client operations |
| `web/src/pages/AppDetail.tsx` | Main app dashboard UI |
| `web/src/api/client.ts` | Frontend API client |
| `infra/eksctl-test-cluster.yaml` | Test cluster provisioning |

### Database Schema

```sql
-- Core entities
projects (id, name, created_at)
clusters (id, project_id, name, endpoint, kubeconfig_encrypted, status)
apps (id, cluster_id, name, namespace, image, replicas, port, env_vars,
      cpu_request, cpu_limit, memory_request, memory_limit,
      health_path, health_port, health_initial_delay, health_period,
      current_revision, status, created_at, updated_at,
      domain, domain_status, pre_deploy_command)
app_revisions (id, app_id, revision_number, image, replicas, ...,
      domain, pre_deploy_command)
app_secrets (id, app_id, key, encrypted_value, created_at, updated_at)
api_tokens (id, token_hash, name, created_at)
```

---

## Upcoming Phases (v1.0+)

### Phase 2.11: Zero-Downtime Deployment Defaults (P0)

**Status**: Planning
**Priority**: P0 (observed production issue: `prod-coding-discovery` currently has no probes, no PDB, no topology spread — a routine node drain would cause a full outage)

Shipit's value proposition is that users don't write Kubernetes YAML. The corollary: shipit must produce production-safe Deployments by default. Every app should survive a node drain, a rolling update, and a pod OOM without any user-visible downtime.

#### 2.11.0 Observed Problem (Motivation)

Cluster audit of `prod-coding-discovery` (4 replicas, managed-by: shipit) on 2026-04-18:

| Gap | Impact |
|---|---|
| No `readinessProbe` | Ingress routes to containers the instant they start → 502s during rolling updates |
| No `livenessProbe` | Hung/deadlocked pods stay in rotation forever |
| No `PodDisruptionBudget` | Node drain (cluster upgrade, Karpenter scale-down, spot reclaim) can evict all 4 pods at once → full outage |
| No `topologySpreadConstraints` / `podAntiAffinity` | Today's 4-node spread is coincidence; reschedule could pile all pods on one node |
| No `preStop` hook | Container exits before ingress endpoint-slice propagation (~5s) → dropped in-flight requests during rollout |
| Image `:latest` + `imagePullPolicy: Always` | Mixed-digest fleet; same Deployment, different code per pod |
| No auto-rollback | Failed rollouts stay half-applied |

DB schema already has `health_path/port/initial_delay/period` columns — they're optional and unset for this app. Optional ≠ safe default.

#### 2.11.1 Design Principle — "Can't deploy unsafely even if you try"

Three tiers of smart-ness:

**Tier 1 — Required at app creation (block deploy if missing)**:
- `health_path` (probe endpoint) — fall back to TCP probe on container port only if explicitly opted out
- `resource.cpu_request`, `resource.memory_request` — without these, pods are BestEffort class = first to evict under pressure
- `max_request_duration_seconds` — derives termination grace period + ingress timeouts + progress deadline

**Tier 2 — Fully automated from Tier 1 inputs (no user input)**:
- PDB, topology spread, preStop, probes, surge/unavail, priority class, image tagging

**Tier 3 — Observed & auto-tuned over time**:
- `startupProbe` thresholds (from first-deploy cold-start p99)
- Resource right-sizing suggestions (VPA-style, observed actual)
- Termination grace tuning (observed in-flight request drain time)

#### 2.11.2 Parameter Derivation Logic

| Parameter | Derivation | Notes |
|---|---|---|
| `readinessProbe` | HTTP GET on `health_path`; `periodSeconds: 5`, `timeoutSeconds: 3`, `failureThreshold: 3`, `successThreshold: 1` | Mandatory. If no `health_path`, fall back to TCP check on declared port. |
| `livenessProbe` | Same endpoint, `periodSeconds: 10`, `failureThreshold: 5`, `initialDelaySeconds: 30` | Separate from readiness so slow-starters don't get killed during warmup. |
| `startupProbe` | Same endpoint, `periodSeconds: 5`, `failureThreshold: ceil(p99_coldstart / 5) + 3` | Auto-measure p99 on first deploy; re-tune every deploy. |
| `PodDisruptionBudget` | `minAvailable = max(replicas - 1, 1)` if replicas ≥ 2; skip if replicas = 1 | Always created as sibling resource when replicas ≥ 2. |
| `topologySpreadConstraints` | Two constraints: `topology.kubernetes.io/zone` and `kubernetes.io/hostname`, `maxSkew: 1`, `whenUnsatisfiable: ScheduleAnyway` | Universal. No app knowledge needed. |
| `terminationGracePeriodSeconds` | `max_request_duration_seconds + 10s buffer`; floor 30s | User-declared "longest request" (30s default, 120s for LLM/streaming apps). |
| `preStop` hook | `exec: ["/bin/sh", "-c", "sleep 5"]` | Covers endpoint-slice propagation to ingress. Single biggest win against rolling-update 5xx. |
| `maxSurge` / `maxUnavailable` | `replicas ≤ 3` → `maxSurge: 1, maxUnavailable: 0`; else `25% / 25%` | Never lose capacity for small fleets. |
| `progressDeadlineSeconds` | `max(600, startup_p99 * 3 * replicas)` | Fail visibly if rollout stalls. |
| `revisionHistoryLimit` | `10` | Enables rollback chain. |
| `image` | `repo:<sha>` (from Phase 5) | Never `:latest`. |
| `imagePullPolicy` | `IfNotPresent` | Safe with immutable tags. |
| `priorityClassName` | `shipit-production` (defined once at cluster level, non-zero) | Preferred under cluster pressure over batch jobs. |
| HPA `minReplicas` | `max(configured, 2)` for prod apps | Prevent scale-to-one that breaks PDB + creates SPOF. |
| Ingress `proxy-next-upstream` | `error timeout http_502 http_503 http_504` | Retry around pods in-flight of termination. |
| Ingress `proxy-read-timeout` | `max_request_duration_seconds + 5s` | Don't cut off legitimate long requests. |
| ALB deregistration delay | `terminationGracePeriodSeconds` (or ≥ that) | Set via ingress annotation if shipit owns ALB config. |

#### 2.11.3 Implementation Scope

- [ ] **Deployment renderer** (`internal/k8s/render.go` or similar): single function `RenderDeployment(app) (*appsv1.Deployment, *policyv1.PodDisruptionBudget, *autoscalingv2.HorizontalPodAutoscaler)` that emits all three resources with the derivations above
- [ ] **DB schema**: add `max_request_duration_seconds` (int), `startup_p99_ms` (int, auto-populated), keep existing probe columns
- [ ] **App creation validation**: reject creation if `health_path` or `resource_*` missing (API 400 with clear remediation message)
- [ ] **Migration for existing apps**: one-time job that backfills sane defaults for all apps currently managed by shipit; show a "health warning" in UI for apps where the renderer would change spec (diff preview before apply)
- [ ] **PDB creation**: ensure shipit has RBAC to manage `policy/v1 PodDisruptionBudget` in app namespaces
- [ ] **Startup probe auto-tuning**: on each deploy, measure time from pod `Ready=False → Ready=True`; persist p99 per app; feed into next render
- [ ] **Auto-rollback**: watch rollout; if new ReplicaSet fails to become Available within `progressDeadlineSeconds`, trigger `kubectl rollout undo` + mark deploy failed + Slack alert
- [ ] **Health endpoint enforcement**: during deploy, after rollout completes, curl the `health_path` through the service → must return 2xx before marking deploy "successful"
- [ ] **UI**: app creation form shows expandable "Advanced — reliability" section pre-populated with derived values, read-only but with "see reasoning" tooltips; app detail page shows current reliability posture (probes ✓/✗, PDB ✓/✗, etc.)
- [ ] **Pre-flight check command**: `shipit app verify <id>` — reports any gaps between rendered ideal and actual cluster state (run after manual `kubectl edit` or drift)
- [ ] **Documentation**: "Reliability Guarantees" page in README describing the exact defaults shipit applies

#### 2.11.4 Acceptance Criteria

A fresh app deployed via shipit should pass this test without any user effort:

1. Cordon + drain any node hosting one of the app's pods — zero 5xx observed at the ingress.
2. Kill a running pod (`kubectl delete pod`) — zero 5xx observed.
3. Deploy a known-broken image (health endpoint returns 500) — deploy auto-rolls back within 10 min; previous version stays live; zero 5xx.
4. Simultaneously scale the cluster down by 50% (Karpenter consolidation) — PDB prevents full outage; ≥ replicas-1 pods stay available.
5. Deploy 10 commits to main in rapid succession — only the newest SHA ends up running (debounced); no mixed-digest fleet.

---



**Goal**: Migrate existing Porter apps to shipit while running both systems in parallel

#### 3.1 Migration Script & Observer Mode
**Status**: Planning
**Priority**: P0

Import Porter apps and observe deployments before taking over.

**Design Decisions**:

| Decision | Choice |
|----------|--------|
| Source of truth | K8s primary, Porter for gaps (pre-deploy commands) |
| App scope | All Porter apps (read-only sync is safe) |
| Detection method | Porter labels/Helm naming pattern |
| Observer behavior | Auto-update shipit DB when Porter deploys |
| Architecture | Background worker in shipit (goroutine polling K8s) |
| Switchover | Per-app toggle ("managed by shipit" flag) |
| History import | No past history, track new deployments going forward |

**Implementation Scope**:
- [ ] Background worker to watch K8s deployments
- [ ] Detect Porter-managed apps via labels
- [ ] Import current state: image, replicas, resources, env vars from K8s
- [ ] Fetch pre-deploy commands from Porter API
- [ ] Auto-create shipit app records
- [ ] Track deployment events (image changes)
- [ ] Per-app "managed_by" field (porter/shipit)
- [ ] UI toggle to switch management

---

### Phase 4: Observability & Alerts

#### 4.1 Slack Notifications
**Status**: Planning
**Priority**: P1

Send deployment notifications to Slack channels.

**Design Decisions**:

| Decision | Choice |
|----------|--------|
| Events to notify | Global default + per-app configurable override |
| Channel | Single global channel (e.g., #deployments) |
| Integration | Incoming Webhook (simple, no OAuth) |
| Content | Standard: app name, image tag, deployed by, status, dashboard link |

**Event Options** (configurable):
- Deploy started
- Pre-deploy hook running
- Deploy success
- Deploy failure
- Rollback

**Implementation Scope**:
- [ ] Add `slack_webhook_url` to config/settings
- [ ] Add `slack_events` global setting (bitmask or JSON array)
- [ ] Add `slack_events` per-app override field
- [ ] Slack notification service/function
- [ ] Call on deploy events
- [ ] Settings UI for Slack configuration

---

#### 4.2 Audit Logs
**Status**: Planning
**Priority**: P1

Track deployment actions for compliance and debugging.

**Design Decisions**:

| Decision | Choice |
|----------|--------|
| Scope | Deployments only (deploy, rollback, config changes) |
| Storage | PostgreSQL table |
| Retention | 30 days (auto-cleanup) |
| UI | Per-app "History" tab |

**Logged Events**:
- Deploy (image change)
- Rollback
- Env var changes
- Secret changes
- Scaling changes (replicas, HPA)
- Pre-deploy hook config changes

**Implementation Scope**:
- [ ] Create `audit_logs` table (app_id, user_id, action, details, timestamp)
- [ ] Add audit logging to deploy/rollback handlers
- [ ] Add audit logging to config change handlers
- [ ] Cleanup job for 30-day retention
- [ ] GET /api/apps/{id}/history endpoint
- [ ] History tab in app detail UI

---

### Phase 5: Branch Tracking & CI/CD Integration

**Status**: Planning
**Priority**: P0 (observed production issue: mixed image digests across pods in same Deployment; no auto-deploy on main merge)

Every shipit app should declare a git repo + branch and be automatically redeployed when that branch advances. This is the core PaaS promise (Heroku/Render/Fly/Railway parity) — without it, shipit is a fancy `kubectl apply` wrapper.

#### 5.0 Observed Problem (Motivation)

Audit of `prod-coding-discovery` on 2026-04-18 found:
- Deployment image spec is `…/coding-discovery` (no tag) + `imagePullPolicy: Always` → effectively mutable `:latest`.
- CI pushes new images on every main merge (`:<sha>` + moves `:latest`), but **shipit never patches the Deployment's image tag**.
- Consequence: 4 running pods → 4 different image digests, **none matching current `:latest`**. Each pod picked up a different `:latest` whenever it happened to restart.
- `kubectl rollout undo` is useless (both revisions reference the same mutable tag).

Root cause: shipit treats image as a one-time field at app creation, not a continuously-reconciled output of "HEAD of tracked branch."

#### 5.1 Branch Tracking (P0)

**Design Decisions**:

| Decision | Choice |
|----------|--------|
| Deploy trigger | GitHub webhook (push event) — instant, not poll-based |
| Fallback trigger | CI calls shipit deploy API with explicit `:<sha>` (works pre-webhook) |
| Image tag | Always immutable `:<sha>`, never `:latest` |
| imagePullPolicy | `IfNotPresent` (since tags are immutable) |
| Per-app config | `repo_url`, `tracked_branch`, `deploy_on_push` (bool), `build_context`, `dockerfile_path` |
| Multi-branch | Support N apps pointing at same repo, different branches (prod=main, staging=develop) |
| Auth | GitHub App (preferred) or fine-grained PAT stored encrypted |

**Implementation Scope**:

- [ ] **DB schema**: add columns to `apps`: `repo_url`, `tracked_branch`, `deploy_on_push`, `github_installation_id`, `last_deployed_sha`, `last_deployed_at`
- [ ] **New table** `repositories`: id, url, installation_id, webhook_secret, connected_by_user_id, connected_at
- [ ] **GitHub App registration** (shipit as a GitHub App; install per org; stores installation_id per repo)
- [ ] **Webhook endpoint** `POST /api/webhooks/github` with HMAC verification (`X-Hub-Signature-256`)
- [ ] **Webhook handler**: parse push event → find apps where `repo_url == payload.repository + tracked_branch == payload.ref.replace('refs/heads/','')` → enqueue deploy
- [ ] **Deploy worker**: pulls job → patches Deployment `spec.template.spec.containers[0].image = repo:<sha>` → `kubectl rollout status` with timeout → writes revision → notifies Slack (phase 4.1 tie-in)
- [ ] **Image build responsibility (decision required)**:
  - Option A: CI builds + pushes, shipit webhook only triggers image-swap → simpler, user owns Dockerfile
  - Option B: shipit runs its own build worker (clone → docker build → push to ECR) → Heroku-like, heavier infra
  - Recommend A for v1, B for v2
- [ ] **Concurrency**: queue deploys per app (FIFO); new pushes during in-flight deploy → drop intermediate, keep only newest (last-write-wins with debounce)
- [ ] **Idempotency**: if `last_deployed_sha == incoming_sha`, skip
- [ ] **Manual deploy API** (CI fallback): `POST /api/apps/{id}/deploy {sha, image}` — same code path as webhook
- [ ] **UI**: on app detail page, show tracked branch, last deployed SHA, "Deploy latest" button, deploy history
- [ ] **Rollback**: `kubectl rollout undo` works correctly once images are SHA-pinned
- [ ] **Auto-rollback on failure**: if readiness of new ReplicaSet fails for >`progressDeadlineSeconds`, automatic `rollout undo` + mark deploy failed
- [ ] **Remove `:latest` usage**: migration step — for every existing app, resolve current `:latest` digest to its SHA tag, update deployment spec

**Example Workflow (CI fallback until webhook ships)**:
```yaml
name: Deploy to Shipit
on:
  push:
    branches: [main]
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - name: Build & push
        run: |
          docker build -t $REGISTRY/$REPO:${{ github.sha }} .
          docker push $REGISTRY/$REPO:${{ github.sha }}
      - name: Trigger shipit deploy
        run: |
          curl -fsS -X POST "https://shipit.unboundsec.dev/api/apps/$APP_ID/deploy" \
            -H "Authorization: Bearer ${{ secrets.SHIPIT_TOKEN }}" \
            -H "Content-Type: application/json" \
            -d '{"sha": "${{ github.sha }}", "image": "'$REGISTRY'/'$REPO':${{ github.sha }}"}'
```

#### 5.2 Deploy Observability

- [ ] Deploy timeline per app (commit → build → image push → rollout start → rollout complete or failed)
- [ ] Surface each stage's duration in UI
- [ ] Emit Slack event on deploy-succeeded / deploy-failed / auto-rolled-back

---

### Phase 6: Advanced Features (Separate Planning)

#### 6.1 Canary Deployments
**Status**: Future
**Priority**: P3

Traffic splitting and gradual rollouts.

**Design Decisions**: Requires dedicated planning session

---

## Phase 2: Production Readiness

### P1 - Core Production Features

| Feature | Description | Status |
|---------|-------------|--------|
| **Health checks** | Liveness and readiness probes configuration per app | ✅ Done |
| **Rollbacks** | Revert to previous app revision | ✅ Done |
| **Resource limits** | CPU/memory requests and limits per app | ✅ Done |
| **HPA (auto-scaling)** | Horizontal Pod Autoscaler with min/max replicas and CPU target | ✅ Done |

### P2 - Enhanced Operations

| Feature | Description | Status |
|---------|-------------|--------|
| **App revisions** | Track configuration changes, enable rollback to specific versions | ✅ Done |
| **Ingress per app** | Custom domains with automatic TLS certificates for deployed apps | ✅ Done |
| **Metrics/monitoring** | Prometheus metrics endpoint, resource usage tracking | Planned |
| **Namespaces** | Organize apps into namespaces within clusters | Planned |

---

## Phase 3: Developer Experience

| Feature | Description | Status | Effort |
|---------|-------------|--------|--------|
| **Web dashboard** | Browser-based UI for managing projects, clusters, and apps | ✅ Done | Large |
| **Git-based deploy** | Webhook triggers, automatic builds on push | Planned | Medium |
| **Buildpacks** | Zero-config builds with automatic language detection | Planned | Medium |
| **Preview environments** | Temporary environments for pull requests | Planned | Medium |

---

## Phase 4: Enterprise Features

| Feature | Description | Status | Effort |
|---------|-------------|--------|--------|
| **User management** | User accounts with Google SSO authentication | ✅ Done | Medium |
| **SSO/OAuth** | Google authentication with domain restriction | ✅ Done | Medium |
| **User API tokens** | User-generated tokens for CLI authentication | ✅ Done | Medium |
| **Team/roles** | Multi-user support with RBAC (viewer, developer, admin, owner) | Planned | Medium |
| **Notifications** | Deployment alerts via Slack, email, webhooks | Planned | Small |
| **Audit logs** | Track all user actions for compliance | Planned | Small |
| **Add-ons marketplace** | Managed databases (PostgreSQL, Redis, etc.) | Planned | Large |

---

## Architecture Gap Analysis

### What We Have vs Target

| Component | Target | Current State | Gap |
|-----------|--------|---------------|-----|
| **API Server** | Stateless, middleware chain, auth | ✅ Chi router, middleware, JSON API | Minimal |
| **CLI Client** | Cross-platform, config file | ✅ Cobra framework, full CRUD | Minimal |
| **Web Dashboard** | React SPA | ✅ React + TypeScript + TanStack Query | Done |
| **Authentication** | OAuth, API tokens, sessions | ✅ Google OAuth + sessions + user tokens | Minimal |
| **Authorization** | RBAC with policy engine | 🔴 None | Full RBAC needed |
| **Provisioner Service** | Create clusters via IaC | 🟡 Connect existing only | No cluster creation |
| **Background Workers** | Job queue with retry logic | 🔴 Synchronous only | No async processing |
| **Cache Layer** | Redis for performance | 🔴 Direct DB queries | No caching |
| **Message Queue** | NATS/RabbitMQ | 🔴 None | No job queue |
| **Distributed Tracing** | OpenTelemetry | 🔴 Basic logging only | No tracing |
| **Metrics Export** | Prometheus endpoint | 🔴 None | No metrics |

### Security Gaps

| Feature | Target | Current | Risk Level |
|---------|--------|---------|------------|
| Session management | HTTP-only cookies, CSRF | ✅ HTTP-only cookies, secure sessions | Done |
| OAuth providers | GitHub, Google, GitLab | ✅ Google OAuth with domain restriction | Done |
| Role-based access | Viewer/Developer/Admin/Owner | Single user = full access | **High** |
| Audit logging | All user actions logged | None | Medium |
| Rate limiting | Per-user, per-endpoint | None | Low |

---

## Implementation Details

### HPA Auto-scaling (P1) - ✅ COMPLETED

```bash
shipit apps create <cluster-id> \
  --name myapp \
  --image nginx \
  --min-replicas 2 \
  --max-replicas 10 \
  --cpu-target 70
```

**Implementation Steps:**
1. Database migration:
```sql
ALTER TABLE apps ADD COLUMN min_replicas INTEGER;
ALTER TABLE apps ADD COLUMN max_replicas INTEGER;
ALTER TABLE apps ADD COLUMN cpu_target INTEGER;

ALTER TABLE app_revisions ADD COLUMN min_replicas INTEGER;
ALTER TABLE app_revisions ADD COLUMN max_replicas INTEGER;
ALTER TABLE app_revisions ADD COLUMN cpu_target INTEGER;
```

2. Update `internal/k8s/client.go` to create HorizontalPodAutoscaler:
```go
hpa := &autoscalingv2.HorizontalPodAutoscaler{
    ObjectMeta: metav1.ObjectMeta{Name: app.Name, Namespace: app.Namespace},
    Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
        ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
            APIVersion: "apps/v1",
            Kind:       "Deployment",
            Name:       app.Name,
        },
        MinReplicas: &app.MinReplicas,
        MaxReplicas: app.MaxReplicas,
        Metrics: []autoscalingv2.MetricSpec{{
            Type: autoscalingv2.ResourceMetricSourceType,
            Resource: &autoscalingv2.ResourceMetricSource{
                Name: corev1.ResourceCPU,
                Target: autoscalingv2.MetricTarget{
                    Type:               autoscalingv2.UtilizationMetricTargetType,
                    AverageUtilization: &app.CPUTarget,
                },
            },
        }},
    },
}
```

3. Add CLI flags in `cmd/shipit/apps.go`
4. Add UI controls in `web/src/pages/AppDetail.tsx`

### Ingress Per App (P2) - ✅ COMPLETED

**API Endpoints:**
- `GET /apps/{id}/domain` - Get domain status and Ingress info
- `PUT /apps/{id}/domain` - Set/update/remove custom domain

**Implementation:**
1. Database migration (`006_custom_domains.sql`):
```sql
ALTER TABLE apps ADD COLUMN domain VARCHAR(255);
ALTER TABLE apps ADD COLUMN domain_status VARCHAR(50);
ALTER TABLE app_revisions ADD COLUMN domain VARCHAR(255);
```

2. K8s Ingress operations in `internal/k8s/client.go`:
```go
// CreateOrUpdateIngress creates/updates Ingress with TLS via cert-manager
func (c *Client) CreateOrUpdateIngress(name, namespace, domain string, port int) error
func (c *Client) GetIngress(name, namespace string) (*IngressStatus, error)
func (c *Client) DeleteIngress(name, namespace string) error
```

3. Frontend Domain tab with:
   - Status indicator (pending/provisioning/active)
   - Domain configuration form
   - DNS setup instructions with load balancer endpoint

**Requirements:**
- nginx-ingress controller installed in cluster
- cert-manager with Let's Encrypt ClusterIssuer
- DNS CNAME pointing to cluster ingress load balancer

### Pre-deploy Hooks - ✅ COMPLETED

**API Endpoints:**
- `GET /apps/{id}/predeploy` - Get pre-deploy hook configuration
- `PUT /apps/{id}/predeploy` - Set/update/remove pre-deploy command

**Implementation:**
1. Database migration (`008_pre_deploy_hooks.sql`):
```sql
ALTER TABLE apps ADD COLUMN pre_deploy_command TEXT;
ALTER TABLE app_revisions ADD COLUMN pre_deploy_command TEXT;
```

2. K8s Job operations in `internal/k8s/client.go`:
```go
// RunPreDeployHook runs a pre-deploy command as a Kubernetes Job
// Returns success/failure and captured logs
func (c *Client) RunPreDeployHook(name, namespace, image, command string) (bool, string, error)
```

3. Deployment flow in `internal/api/handlers.go`:
```go
// DeployApp now checks for pre_deploy_command
// If present, runs as K8s Job before deployment
// Deployment fails if pre-deploy hook fails
```

4. Frontend Hooks tab with:
   - Current hook status display
   - Command configuration form
   - Common examples (migrations, cache warm-up, asset compilation)
   - Documentation and best practices

**Use Cases:**
- Database migrations: `python manage.py migrate`
- Asset compilation: `npm run build`
- Cache warming: `./scripts/warm-cache.sh`
- Health pre-checks: `./scripts/pre-deploy-check.sh`

**Porter Import Notes:**
- `staging-celery` and `prod-celery` apps have pre-deploy: `bash ./migrate.sh`
- Other apps have no pre-deploy hooks configured
- Pre-deploy is a Porter abstraction stored in their DB, not in K8s directly

### Git-Based Deploy (Phase 3)

**Architecture:**
```
GitHub Webhook → API Server → Build Queue → Build Worker → ECR → Deploy
```

**Implementation:**
1. Add `repositories` table (git_url, branch, dockerfile_path)
2. Webhook endpoint `/api/webhooks/github`
3. Background worker to:
   - Clone repo
   - Build Docker image
   - Push to ECR
   - Update app image and deploy
4. GitHub App or OAuth for repo access

### RBAC Implementation (Phase 4)

**Role Hierarchy:**
```
Owner (full access, billing, delete project)
  └── Admin (manage team, settings)
        └── Developer (deploy, manage apps)
              └── Viewer (read-only)
```

**Database Schema:**
```sql
CREATE TABLE users (
    id UUID PRIMARY KEY,
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255),
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE project_members (
    project_id UUID REFERENCES projects(id),
    user_id UUID REFERENCES users(id),
    role VARCHAR(50) NOT NULL, -- owner, admin, developer, viewer
    PRIMARY KEY (project_id, user_id)
);
```

**Permission Matrix:**

| Action | Viewer | Developer | Admin | Owner |
|--------|--------|-----------|-------|-------|
| View apps/clusters | ✓ | ✓ | ✓ | ✓ |
| Deploy/rollback | | ✓ | ✓ | ✓ |
| Manage secrets | | ✓ | ✓ | ✓ |
| Manage team | | | ✓ | ✓ |
| Delete project | | | | ✓ |

### Background Workers (Infrastructure)

**Purpose:** Async job processing for long-running tasks

**Implementation Options:**
1. **Simple:** Goroutine pool with database-backed job queue
2. **Robust:** NATS JetStream or Redis-based queue

**Job Types:**
- Deployment pipeline execution
- Resource cleanup/garbage collection
- Metrics aggregation
- Notification delivery

### Prometheus Metrics (P2)

**Endpoint:** `GET /metrics`

**Metrics to Export:**
```
shipit_apps_total{project="...", cluster="..."}
shipit_deployments_total{app="...", status="success|failed"}
shipit_api_requests_total{method="...", path="...", status="..."}
shipit_api_request_duration_seconds{method="...", path="..."}
```

**Implementation:**
- Use `prometheus/client_golang`
- Add middleware to track request metrics
- Expose `/metrics` endpoint (unauthenticated or separate port)

---

## Recommended Implementation Order

### Immediate (Testing Infrastructure)
1. **Test Cluster** - Provision AWS EKS test cluster (`eksctl create cluster -f infra/eksctl-test-cluster.yaml`)
2. **Deploy Test App** - Simple nginx app for testing new features

### Short-term (Complete P1) - ✅ DONE
3. **HPA Auto-scaling** - ✅ Completed (2025-01-03)

### Dashboard Enhancements
4. **Live K8s Status** - Show real pod status from cluster (not just DB status)
5. **Logs Tab** - Wire StreamLogs backend to UI
6. **Scaling UI** - Adjust replica count from dashboard
7. **Rollback UI** - Visual revision history with one-click rollback

### Medium-term (P2 Features)
8. **Ingress per App** - Custom domains with TLS
9. **Prometheus Metrics** - `/metrics` endpoint for monitoring

### Later (Phase 3-4)
10. **Git-based Deploy** - Webhooks, auto-build on push
11. **RBAC** - Roles and permissions (HIGH PRIORITY for security)
12. ~~**OAuth/SSO**~~ - ✅ Google SSO complete (v0.9.0)
13. **Notifications** - Slack, email alerts

---

## API Routes Reference

```
# Public
GET  /health

# Authentication (v0.9.0)
GET  /auth/login           # Redirect to Google OAuth
GET  /auth/callback        # OAuth callback
POST /auth/logout          # End session

# User Profile & Tokens (v0.9.0)
GET    /api/me             # Get current user
GET    /api/tokens         # List user's API tokens
POST   /api/tokens         # Create new API token
DELETE /api/tokens/{id}    # Revoke API token

# Projects
GET    /api/projects
POST   /api/projects
GET    /api/projects/{projectID}
DELETE /api/projects/{projectID}

# Clusters
GET    /api/projects/{projectID}/clusters
POST   /api/projects/{projectID}/clusters
GET    /api/clusters/{clusterID}
DELETE /api/clusters/{clusterID}

# Apps
GET    /api/clusters/{clusterID}/apps
POST   /api/clusters/{clusterID}/apps
GET    /api/apps/{appID}
PUT    /api/apps/{appID}
PATCH  /api/apps/{appID}
DELETE /api/apps/{appID}
POST   /api/apps/{appID}/deploy
GET    /api/apps/{appID}/status
GET    /api/apps/{appID}/logs?tail=100&follow=true
POST   /api/apps/{appID}/rollback
GET    /api/apps/{appID}/autoscaling
PUT    /api/apps/{appID}/autoscaling
GET    /api/apps/{appID}/domain
PUT    /api/apps/{appID}/domain
GET    /api/apps/{appID}/predeploy
PUT    /api/apps/{appID}/predeploy

# Revisions
GET    /api/apps/{appID}/revisions
GET    /api/apps/{appID}/revisions/{revision}

# Secrets
GET    /api/apps/{appID}/secrets
POST   /api/apps/{appID}/secrets
DELETE /api/apps/{appID}/secrets/{key}
```

---

## Technology Stack

| Layer | Technology | Purpose |
|-------|------------|---------|
| Backend | Go + Chi | API server, stateless, fast |
| Frontend | React + TypeScript | SPA with TanStack Query |
| Database | PostgreSQL | Primary data store |
| Orchestration | Kubernetes | Container orchestration |
| Container Registry | AWS ECR | Docker image storage |
| Infrastructure | AWS EKS | Managed Kubernetes |
| Encryption | AES-256-GCM | Secrets and kubeconfig |
| Auth | Google OAuth + Sessions + User Tokens | Authentication |

---

## Architecture Patterns

### System Overview

```
┌─────────────────────────────────────────────────────────────────────────┐
│                           USER INTERFACES                                │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐    │
│  │  Web        │  │  Command    │  │  REST       │  │  CI/CD      │    │
│  │  Dashboard  │  │  Line Tool  │  │  API        │  │  Webhooks   │    │
│  └─────────────┘  └─────────────┘  └─────────────┘  └─────────────┘    │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                           API SERVER                                     │
│  ┌─────────────────────────────────────────────────────────────────┐   │
│  │  Authentication │ Authorization │ Rate Limiting │ Request Routing │  │
│  └─────────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
          ┌─────────────────────────┼─────────────────────────┐
          ▼                         ▼                         ▼
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│   CORE API      │     │   PROVISIONER   │     │   BACKGROUND    │
│   SERVER        │     │   SERVICE       │     │   WORKERS       │
│                 │     │   (future)      │     │   (future)      │
│ • App Mgmt      │     │ • Infra Create  │     │ • Async Jobs    │
│ • User Mgmt     │     │ • Infra Update  │     │ • Scheduled     │
│ • Cluster Ops   │     │ • State Mgmt    │     │   Tasks         │
│ • Release Mgmt  │     │ • Cloud APIs    │     │ • Cleanup       │
└─────────────────┘     └─────────────────┘     └─────────────────┘
          │                         │                         │
          └─────────────────────────┼─────────────────────────┘
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                        DATA & INTEGRATION LAYER                          │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐ │
│  │ Database │  │ Cache    │  │ Object   │  │ Message  │  │ Secrets  │ │
│  │ (PG)     │  │ (future) │  │ Storage  │  │ Queue    │  │ (AES)    │ │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘  └──────────┘ │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                      MANAGED INFRASTRUCTURE                              │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐         │
│  │  AWS EKS        │  │  (GKE future)   │  │  (AKS future)   │         │
│  │  Clusters       │  │                 │  │                 │         │
│  └─────────────────┘  └─────────────────┘  └─────────────────┘         │
└─────────────────────────────────────────────────────────────────────────┘
```

### Design Principles

1. **Multi-Tenancy First**: Every component designed for isolation between projects
2. **Cloud Agnostic**: Abstract cloud-specific implementations behind common interfaces
3. **API-First**: All functionality accessible via REST API
4. **Event-Driven**: Asynchronous processing for long-running operations (future)
5. **Stateless Services**: Horizontal scalability for all service components
6. **Infrastructure as Code**: All provisioning declarative and version-controlled

### Request Processing Pipeline

```
Request → Auth → Authz → Validation → Handler → Response
            │       │         │           │
            ▼       ▼         ▼           ▼
         Token    Policy   Schema     Business
         Check   (future)  Validator   Logic
```

**Current Middleware Chain:**
1. **Logger Middleware**: Request/response logging
2. **Recovery Middleware**: Catches panics, returns 500
3. **RequestID Middleware**: Assigns correlation ID
4. **Auth Middleware**: Validates API token, loads context
5. **JSON Middleware**: Sets Content-Type for API routes

### Entity Relationships

```
┌──────────┐         ┌──────────┐         ┌──────────┐
│  Project │────────▶│  Cluster │────────▶│   App    │
└──────────┘   1:N   └──────────┘   1:N   └──────────┘
                                               │
                                    ┌──────────┼──────────┐
                                    │          │          │
                                    ▼          ▼          ▼
                             ┌──────────┐ ┌──────────┐ ┌──────────┐
                             │ Revision │ │  Secret  │ │  Status  │
                             └──────────┘ └──────────┘ └──────────┘
```

### Application Types (Future Support)

| Type | Description | Use Case |
|------|-------------|----------|
| Web Service | HTTP-serving application | APIs, websites |
| Worker | Background processing | Queue consumers, batch jobs |
| Cron Job | Scheduled execution | Reports, cleanup |
| One-off Task | Single execution | Migrations, scripts |

### Deployment Methods

| Method | Current Status | Description |
|--------|---------------|-------------|
| Container Image | ✅ Supported | Pull from ECR/any registry |
| Git-Based | 🔴 Planned | Webhook triggers, auto-build |
| Buildpacks | 🔴 Planned | Zero-config, language detection |

### Security Architecture

**Transport Security:**
- TLS 1.2+ required for all connections (via AWS ALB)
- HTTPS only for production

**Secrets Management:**
- All sensitive data encrypted at rest (AES-256-GCM)
- Encryption key stored as environment variable
- Secrets never returned in API responses (write-only)

**Authentication Flow:**
```
Client                          API Server
  │                                  │
  │──── Request + Bearer Token ─────▶│
  │                                  │
  │                            ┌─────┴─────┐
  │                            │  Validate │
  │                            │   Token   │
  │                            └─────┬─────┘
  │                                  │
  │◀──── Response ──────────────────│
  │                                  │
```

**Target Auth Methods (Future):**
1. Session-Based: HTTP-only cookies, CSRF protection
2. OAuth 2.0: GitHub, Google, GitLab
3. API Tokens: Long-lived bearer tokens (current)
4. Service-to-Service: Mutual TLS (future)

### Deployment Architecture

**Container-Based Deployment:**
```
┌─────────────────────────────────────────────────────────────┐
│                    KUBERNETES CLUSTER                        │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐         │
│  │  Shipit     │  │  Shipit     │  │  Shipit     │         │
│  │  API Server │  │  API Server │  │  API Server │         │
│  │  (replica)  │  │  (replica)  │  │  (replica)  │         │
│  └─────────────┘  └─────────────┘  └─────────────┘         │
│                                                              │
│  ┌─────────────────────────────────────────────────────┐   │
│  │                   Managed Apps                        │   │
│  │  ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐    │   │
│  │  │ App 1   │ │ App 2   │ │ App 3   │ │ App N   │    │   │
│  │  └─────────┘ └─────────┘ └─────────┘ └─────────┘    │   │
│  └─────────────────────────────────────────────────────┘   │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

**Build Process (Multi-Stage):**
1. **Build Stage**: Compile Go backend + React frontend
2. **Runtime Stage**: Minimal Alpine image with binaries + static assets

### Monitoring & Observability (Target)

**Metrics to Implement:**
- Request rate, error rate, latency (RED method)
- Active deployments, pod status
- Resource utilization per app

**Logging Strategy:**
- Structured JSON format
- Correlation IDs for tracing
- Log levels: ERROR, WARN, INFO, DEBUG

### High Availability Patterns

**Redundancy:**
- API Server: Multiple replicas behind load balancer
- Database: RDS with multi-AZ (current)
- Apps: Configurable replica count

**Failure Handling:**
- Circuit breakers for external calls (future)
- Retry with exponential backoff (future)
- Graceful degradation
- Health check endpoints (`/health`)

---

## Contributing

When implementing features:
1. Update this roadmap with status changes
2. Add CLI commands to README.md
3. Update API documentation
4. Add database migrations as needed
5. Write tests for new functionality

---

## Version History

| Version | Date | Features |
|---------|------|----------|
| v0.1.0 | Dec 2025 | Core platform, CLI, multi-cluster |
| v0.2.0 | Dec 2025 | Secrets management |
| v0.3.0 | Jan 2026 | Health checks, resource limits |
| v0.4.0 | Jan 2026 | Rollbacks, revisions |
| v0.5.0 | Jan 2026 | Web dashboard, environment variables |
| v0.6.0 | Jan 2026 | HPA auto-scaling |
| v0.7.0 | Jan 2026 | Custom domains & Ingress |
| v0.8.0 | Jan 2026 | Pre-deploy hooks |
| v0.9.0 | Jan 2026 | Google SSO, user tokens, session auth |
| v0.9.1 | Jan 2026 | Default app URLs with auto-TLS Ingress |
