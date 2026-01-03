# Shipit Roadmap

This document tracks planned features, development phases, and architecture gap analysis for shipit.

---

## Current Status Summary

**Version:** v0.4.0
**Last Updated:** January 2026

| Phase | Status | Completion |
|-------|--------|------------|
| Phase 1: Core Platform | ✅ Complete | 100% |
| Phase 2: Production Readiness | 🟡 In Progress | 80% (HPA remaining) |
| Phase 3: Developer Experience | ⬜ Not Started | 0% |
| Phase 4: Enterprise Features | ⬜ Not Started | 0% |

---

## Completed (v0.1.0 - v0.4.0)

### Core Platform
- [x] Core API server with project/cluster/app management
- [x] CLI client with full CRUD operations
- [x] Multi-cluster support (connect existing Kubernetes clusters)
- [x] Container image deployments
- [x] Log streaming
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

---

## Phase 2: Production Readiness

### P0 - Blocking Dogfooding

| Feature | Description | Status |
|---------|-------------|--------|
| **Secrets management** | Secure storage for app secrets (DATABASE_URL, API keys). Stored encrypted in DB, injected as K8s Secrets. | ✅ Done |

### P1 - Core Production Features

| Feature | Description | Status |
|---------|-------------|--------|
| **Health checks** | Liveness and readiness probes configuration per app | ✅ Done |
| **Rollbacks** | Revert to previous app revision | ✅ Done |
| **Resource limits** | CPU/memory requests and limits per app | ✅ Done |
| **HPA (auto-scaling)** | Horizontal Pod Autoscaler with min/max replicas and CPU target | 🔴 Next Up |

### P2 - Enhanced Operations

| Feature | Description | Status |
|---------|-------------|--------|
| **App revisions** | Track configuration changes, enable rollback to specific versions | ✅ Done |
| **Ingress per app** | Custom domains with automatic TLS certificates for deployed apps | Planned |
| **Metrics/monitoring** | Prometheus metrics endpoint, resource usage tracking | Planned |
| **Namespaces** | Organize apps into namespaces within clusters | Planned |

---

## Phase 3: Developer Experience

| Feature | Description | Status | Effort |
|---------|-------------|--------|--------|
| **Web dashboard** | Browser-based UI for managing projects, clusters, and apps | Planned | Large |
| **Git-based deploy** | Webhook triggers, automatic builds on push | Planned | Medium |
| **Buildpacks** | Zero-config builds with automatic language detection | Planned | Medium |
| **Preview environments** | Temporary environments for pull requests | Planned | Medium |

---

## Phase 4: Enterprise Features

| Feature | Description | Status | Effort |
|---------|-------------|--------|--------|
| **User management** | User accounts with email/password authentication | Planned | Medium |
| **Team/roles** | Multi-user support with RBAC (viewer, developer, admin, owner) | Planned | Medium |
| **SSO/OAuth** | GitHub, Google, GitLab authentication | Planned | Medium |
| **Notifications** | Deployment alerts via Slack, email, webhooks | Planned | Small |
| **Audit logs** | Track all user actions for compliance | Planned | Small |
| **Add-ons marketplace** | Managed databases (PostgreSQL, Redis, etc.) | Planned | Large |

---

## Architecture Gap Analysis

Comparison between current implementation and [ARCHITECTURE_REFERENCE.md](./ARCHITECTURE_REFERENCE.md).

### What We Have vs Architecture Target

| Architecture Component | Target | Current State | Gap |
|----------------------|--------|---------------|-----|
| **API Server** | Stateless, middleware chain, auth | ✅ Chi router, middleware, JSON API | Minimal |
| **CLI Client** | Cross-platform, config file | ✅ Cobra framework, full CRUD | Minimal |
| **Authentication** | OAuth, API tokens, sessions | 🟡 API tokens only | Missing OAuth/sessions |
| **Authorization** | RBAC with policy engine | 🔴 None | Full RBAC needed |
| **Provisioner Service** | Create clusters via IaC | 🟡 Connect existing only | No cluster creation |
| **Background Workers** | Job queue with retry logic | 🔴 Synchronous only | No async processing |
| **Web Dashboard** | React SPA | 🔴 None | Full UI needed |
| **Cache Layer** | Redis for performance | 🔴 Direct DB queries | No caching |
| **Message Queue** | NATS/RabbitMQ | 🔴 None | No job queue |
| **Distributed Tracing** | OpenTelemetry | 🔴 Basic logging only | No tracing |
| **Metrics Export** | Prometheus endpoint | 🔴 None | No metrics |

### Missing Infrastructure Components

| Component | Purpose | Priority | Effort |
|-----------|---------|----------|--------|
| **Background Worker Pool** | Async job processing, scheduled tasks, cleanup | High | Medium |
| **Redis Cache** | Session storage, hot data caching | Medium | Small |
| **Message Queue (NATS)** | Job queue, event streaming | Medium | Medium |
| **OpenTelemetry** | Distributed tracing, observability | Low | Medium |

### Security Gaps

| Feature | Target | Current | Risk Level |
|---------|--------|---------|------------|
| Session management | HTTP-only cookies, CSRF | API tokens only | Low |
| OAuth providers | GitHub, Google, GitLab | None | Medium |
| Role-based access | Viewer/Developer/Admin/Owner | Single token = full access | High |
| Audit logging | All user actions logged | None | Medium |
| Rate limiting | Per-user, per-endpoint | None | Low |

---

## Feature Details

### HPA Auto-scaling (P1) - NEXT UP

```bash
shipit apps create <cluster-id> \
  --name myapp \
  --image nginx \
  --min-replicas 2 \
  --max-replicas 10 \
  --cpu-target 70
```

**Implementation:**
- Add `min_replicas`, `max_replicas`, `cpu_target` columns to apps table
- Create HorizontalPodAutoscaler resource alongside Deployment
- Update revision snapshots to include HPA config
- CLI flags for auto-scaling configuration

**Database Migration:**
```sql
ALTER TABLE apps ADD COLUMN min_replicas INTEGER;
ALTER TABLE apps ADD COLUMN max_replicas INTEGER;
ALTER TABLE apps ADD COLUMN cpu_target INTEGER;

ALTER TABLE app_revisions ADD COLUMN min_replicas INTEGER;
ALTER TABLE app_revisions ADD COLUMN max_replicas INTEGER;
ALTER TABLE app_revisions ADD COLUMN cpu_target INTEGER;
```

### Ingress Per App (P2)

```bash
shipit apps create <cluster-id> \
  --name myapp \
  --image nginx \
  --domain myapp.example.com
```

**Implementation:**
- Creates Ingress resource with nginx class
- Uses cert-manager for automatic TLS
- Requires DNS CNAME to cluster ingress LB
- Add `domain` column to apps table

### Secrets Management (P0) - DONE

```bash
# CLI commands
shipit secrets set <app-id> --key DATABASE_URL --value "postgres://..."
shipit secrets list <app-id>
shipit secrets delete <app-id> --key API_KEY
```

**Implementation:**
- `app_secrets` table with encrypted values (AES-256-GCM)
- Secrets created as Kubernetes Secret objects
- Referenced in Deployment via `envFrom.secretRef`
- Never exposed in API responses (write-only)

### Resource Limits (P1) - DONE

```bash
shipit apps create <cluster-id> \
  --name myapp \
  --image nginx \
  --cpu-request 100m \
  --cpu-limit 500m \
  --memory-request 128Mi \
  --memory-limit 512Mi
```

**Defaults:**
- CPU request: 100m, limit: 500m
- Memory request: 128Mi, limit: 256Mi

### Health Checks (P1) - DONE

```bash
shipit apps create <cluster-id> \
  --name myapp \
  --image nginx \
  --health-path /health \
  --health-port 8080 \
  --health-initial-delay 10 \
  --health-period 30
```

**Implementation:**
- HTTP GET probe by default
- Configurable path, port, delays
- Both liveness and readiness use same config

### Rollbacks (P1) - DONE

```bash
# List revisions
shipit apps revisions <app-id>

# Rollback to previous
shipit apps rollback <app-id>

# Rollback to specific revision
shipit apps rollback <app-id> --revision 3
```

**Implementation:**
- App config snapshots stored in `app_revisions` table
- Rollback re-applies previous config and triggers deploy
- Keeps last 10 revisions per app

---

## Recommended Implementation Order

### Immediate (Priority: Testing & Visibility)
1. **Web Dashboard** - React SPA for visual management (enables faster testing of all features)

### Short-term (Complete P1)
2. **HPA Auto-scaling** - Last P1 feature, completes production readiness

### Medium-term (P2 Features)
3. **Ingress per App** - Custom domains with TLS
4. **Prometheus Metrics** - `/metrics` endpoint for monitoring
5. **Namespace Support** - Organize apps within clusters

### Later (Phase 3-4)
6. **Git-based Deploy** - Webhooks, auto-build on push
7. **User Management** - User accounts, email/password auth
8. **RBAC** - Roles and permissions
9. **OAuth/SSO** - GitHub, Google authentication
10. **Notifications** - Slack, email alerts

---

## Architecture Reference

See [ARCHITECTURE_REFERENCE.md](./ARCHITECTURE_REFERENCE.md) for comprehensive architecture documentation including:
- System overview and design principles
- Component deep dives
- Data architecture
- Security architecture
- Integration patterns
- Technology recommendations

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
| v0.5.0 | TBD | HPA auto-scaling |
