# Shipit

A lightweight PaaS for deploying applications to Kubernetes clusters.

## Features

- **Multi-cluster support**: Connect and manage multiple Kubernetes clusters
- **Project organization**: Group clusters and apps into projects
- **Simple deployments**: Deploy container images with a single command
- **Secrets management**: Securely store and inject secrets into applications
- **Log streaming**: Stream logs from deployed applications
- **API-first**: Full REST API with CLI client

## Quick Start

### Prerequisites

- Go 1.21+
- PostgreSQL database
- Kubernetes cluster(s) to deploy to

### Build

```bash
# Build CLI
go build -o shipit ./cmd/shipit

# Build server
go build -o shipit-server ./cmd/server
```

### Run the Server

```bash
export DATABASE_URL="postgres://user:pass@localhost:5432/shipit?sslmode=disable"
export ENCRYPT_KEY="your-32-byte-hex-key"  # Use: openssl rand -hex 32
export PORT=8090

./shipit-server
```

### Configure CLI

```bash
# Set API URL
./shipit config set-url http://localhost:8090

# Set API token (generate in database)
./shipit config set-token <your-token>

# Verify configuration
./shipit config show
```

## CLI Commands

### Projects

```bash
# List all projects
shipit projects list

# Create a project
shipit projects create <name>

# Delete a project
shipit projects delete <id>
```

### Clusters

```bash
# List clusters in a project
shipit clusters list <project-id>

# Connect a cluster
shipit clusters connect <project-id> --name <name> --kubeconfig ~/.kube/config

# Delete a cluster
shipit clusters delete <id>
```

### Applications

```bash
# List apps in a cluster
shipit apps list <cluster-id>

# Create an app (without deploying)
shipit apps create <cluster-id> \
  --name myapp \
  --image nginx:latest \
  --port 80 \
  --namespace default \
  --env KEY=VALUE

# Deploy an existing app
shipit apps deploy <app-id>

# Get app details
shipit apps get <app-id>

# Get deployment status
shipit apps status <app-id>

# Delete an app
shipit apps delete <app-id>
```

### Deploy (create + deploy in one step)

```bash
shipit deploy create <cluster-id> \
  --name myapp \
  --image nginx:latest \
  --port 80 \
  --replicas 2 \
  --namespace production \
  --env DATABASE_URL=postgres://... \
  --env API_KEY=secret
```

### Resource Limits

Set CPU and memory limits for your applications:

```bash
shipit apps create <cluster-id> \
  --name myapp \
  --image nginx:latest \
  --port 80 \
  --cpu-request 100m \
  --cpu-limit 500m \
  --memory-request 128Mi \
  --memory-limit 512Mi
```

**Defaults:**
- CPU request: 100m, limit: 500m
- Memory request: 128Mi, limit: 256Mi

### Health Checks

Configure liveness and readiness probes:

```bash
shipit apps create <cluster-id> \
  --name myapp \
  --image nginx:latest \
  --port 8080 \
  --health-path /health \
  --health-port 8080 \
  --health-initial-delay 10 \
  --health-period 30
```

**Options:**
- `--health-path`: HTTP endpoint to probe (e.g., `/health`)
- `--health-port`: Port to probe (defaults to app port)
- `--health-initial-delay`: Seconds before first probe (default: 10)
- `--health-period`: Seconds between probes (default: 30)

### Secrets

```bash
# List secrets for an app (only keys shown, values are never exposed)
shipit secrets list <app-id>

# Set a secret
shipit secrets set <app-id> --key DATABASE_URL --value "postgres://..."

# Delete a secret
shipit secrets delete <app-id> --key API_KEY
```

> **Note**: After adding/updating/deleting secrets, redeploy the app to apply changes.

### Logs

```bash
# Stream logs
shipit logs <app-id> -f

# Get last N lines
shipit logs <app-id> --tail 100
```

### Revisions and Rollbacks

Shipit automatically tracks deployment revisions. Each deploy creates a snapshot of the app configuration (image, replicas, resources, health checks, env vars).

```bash
# List revisions for an app (shows last 10 by default)
shipit apps revisions <app-id>

# Show more revisions
shipit apps revisions <app-id> --limit 20

# Rollback to the previous revision
shipit apps rollback <app-id>

# Rollback to a specific revision
shipit apps rollback <app-id> --revision 3
```

**Notes:**
- Revisions are created automatically on each deploy
- Up to 10 revisions are kept per app (configurable)
- Rollback re-applies the saved configuration and triggers a new deploy

## API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | /health | Health check |
| GET | /api/projects | List projects |
| POST | /api/projects | Create project |
| GET | /api/projects/:id | Get project |
| DELETE | /api/projects/:id | Delete project |
| GET | /api/projects/:id/clusters | List clusters |
| POST | /api/projects/:id/clusters | Connect cluster |
| GET | /api/clusters/:id | Get cluster |
| DELETE | /api/clusters/:id | Delete cluster |
| GET | /api/clusters/:id/apps | List apps |
| POST | /api/clusters/:id/apps | Create app |
| GET | /api/apps/:id | Get app |
| DELETE | /api/apps/:id | Delete app |
| POST | /api/apps/:id/deploy | Deploy app |
| GET | /api/apps/:id/logs | Stream logs |
| GET | /api/apps/:id/status | Get status |
| GET | /api/apps/:id/secrets | List secrets |
| POST | /api/apps/:id/secrets | Set secret |
| DELETE | /api/apps/:id/secrets/:key | Delete secret |
| GET | /api/apps/:id/revisions | List revisions |
| GET | /api/apps/:id/revisions/:rev | Get revision |
| POST | /api/apps/:id/rollback | Rollback app |

## Database Schema

```sql
-- API tokens for authentication
CREATE TABLE api_tokens (
    id UUID PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    token_hash VARCHAR(64) NOT NULL UNIQUE,
    created_at TIMESTAMP,
    last_used_at TIMESTAMP
);

-- Projects
CREATE TABLE projects (
    id UUID PRIMARY KEY,
    name VARCHAR(255) NOT NULL UNIQUE,
    created_at TIMESTAMP
);

-- Clusters
CREATE TABLE clusters (
    id UUID PRIMARY KEY,
    project_id UUID REFERENCES projects(id),
    name VARCHAR(255) NOT NULL,
    endpoint VARCHAR(512),
    kubeconfig_encrypted BYTEA NOT NULL,
    status VARCHAR(50),
    status_message TEXT,
    created_at TIMESTAMP
);

-- Apps
CREATE TABLE apps (
    id UUID PRIMARY KEY,
    cluster_id UUID REFERENCES clusters(id),
    name VARCHAR(255) NOT NULL,
    namespace VARCHAR(255),
    image VARCHAR(512) NOT NULL,
    replicas INTEGER DEFAULT 1,
    port INTEGER,
    env_vars JSONB,
    status VARCHAR(50),
    status_message TEXT,
    created_at TIMESTAMP,
    updated_at TIMESTAMP,
    -- Resource limits
    cpu_request VARCHAR(50) DEFAULT '100m',
    cpu_limit VARCHAR(50) DEFAULT '500m',
    memory_request VARCHAR(50) DEFAULT '128Mi',
    memory_limit VARCHAR(50) DEFAULT '256Mi',
    -- Health check
    health_path VARCHAR(255),
    health_port INTEGER,
    health_initial_delay INTEGER DEFAULT 10,
    health_period INTEGER DEFAULT 30,
    -- Revision tracking
    current_revision INTEGER DEFAULT 0
);

-- App Secrets (encrypted at rest)
CREATE TABLE app_secrets (
    id UUID PRIMARY KEY,
    app_id UUID REFERENCES apps(id) ON DELETE CASCADE,
    key VARCHAR(255) NOT NULL,
    value_encrypted BYTEA NOT NULL,
    created_at TIMESTAMP,
    updated_at TIMESTAMP,
    UNIQUE(app_id, key)
);

-- App Revisions (deployment history for rollbacks)
CREATE TABLE app_revisions (
    id UUID PRIMARY KEY,
    app_id UUID REFERENCES apps(id) ON DELETE CASCADE,
    revision_number INTEGER NOT NULL,
    image VARCHAR(512) NOT NULL,
    replicas INTEGER DEFAULT 1,
    port INTEGER,
    env_vars JSONB,
    cpu_request VARCHAR(50),
    cpu_limit VARCHAR(50),
    memory_request VARCHAR(50),
    memory_limit VARCHAR(50),
    health_path VARCHAR(255),
    health_port INTEGER,
    health_initial_delay INTEGER,
    health_period INTEGER,
    created_at TIMESTAMP,
    UNIQUE(app_id, revision_number)
);
```

## Deployment

### Kubernetes Manifests

Deployment manifests are in `deploy/k8s/`:

- `shipit-base.yaml` - Base deployment template
- `shipit-ingress.yaml` - Ingress with TLS

### Environment Variables

| Variable | Description | Required |
|----------|-------------|----------|
| DATABASE_URL | PostgreSQL connection string | Yes |
| ENCRYPT_KEY | 32-byte hex key for kubeconfig encryption | Yes |
| PORT | Server port (default: 8090) | No |
| AWS_REGION | AWS region for EKS clusters | No |

## Security

- **Secrets encryption**: All app secrets are encrypted at rest using AES-256-GCM
- **Kubeconfig encryption**: All kubeconfigs are encrypted at rest using AES-256-GCM
- **Token hashing**: API tokens are hashed using SHA-256 before storage
- **Non-root container**: Server runs as non-root user
- **Write-only secrets**: Secret values are never exposed via API responses

## Project Structure

```
shipit/
├── cmd/
│   ├── server/     # API server
│   └── shipit/     # CLI client
├── internal/
│   ├── api/        # HTTP handlers and router
│   ├── auth/       # Authentication and encryption
│   ├── config/     # Configuration loading
│   ├── db/         # Database models and queries
│   └── k8s/        # Kubernetes client and AWS integration
├── deploy/
│   └── k8s/        # Kubernetes manifests
└── migrations/     # Database migrations
```

## License

MIT
