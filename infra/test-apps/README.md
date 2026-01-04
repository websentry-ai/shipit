# Test Applications for ShipIt

This directory contains configurations for test applications used to validate ShipIt functionality during the Porter migration.

## Available Test Apps

### 1. echo-server (Simple Web Service)

A minimal HTTP server that echoes request information.

```bash
# Create the app
shipit apps create <cluster-id> \
  --name echo-server \
  --namespace test \
  --image hashicorp/http-echo:latest \
  --replicas 1 \
  --port 5678

# Deploy
shipit apps deploy <app-id>

# Test
curl http://<pod-ip>:5678
```

### 2. nginx-test (Static Files)

Standard nginx for testing deployments and rollbacks.

```bash
shipit apps create <cluster-id> \
  --name nginx-test \
  --namespace test \
  --image nginx:1.25-alpine \
  --replicas 2 \
  --port 80 \
  --health-path / \
  --health-port 80 \
  --cpu-request 50m \
  --memory-request 64Mi
```

### 3. stress-test (Resource Testing)

For testing HPA and resource limits.

```bash
shipit apps create <cluster-id> \
  --name stress-test \
  --namespace test \
  --image polinux/stress:latest \
  --replicas 1 \
  --cpu-request 100m \
  --cpu-limit 500m \
  --memory-request 128Mi \
  --memory-limit 512Mi
```

### 4. postgres-client (Database Connectivity)

For testing secrets and database connectivity.

```bash
# Create app
shipit apps create <cluster-id> \
  --name db-test \
  --namespace test \
  --image postgres:15-alpine \
  --replicas 1 \
  --port 5432

# Add database secret
shipit secrets create <app-id> \
  --key DATABASE_URL \
  --value "postgres://user:pass@host:5432/db"
```

## Test Scenarios

### Scenario 1: Basic Deployment

1. Create echo-server app
2. Verify pod is running
3. Check logs via `shipit apps logs`
4. Test health endpoint

### Scenario 2: Update & Rollback

1. Deploy nginx:1.24-alpine
2. Update to nginx:1.25-alpine
3. Verify new version running
4. Rollback to previous revision
5. Verify rollback succeeded

### Scenario 3: Secrets Management

1. Create app with secret
2. Verify secret is mounted
3. Update secret value
4. Redeploy and verify updated

### Scenario 4: Resource Limits

1. Deploy stress-test app
2. Run CPU stress: `stress --cpu 2`
3. Verify pod is throttled, not killed
4. Run memory stress
5. Verify OOM behavior

### Scenario 5: Health Checks

1. Deploy app with health endpoint
2. Verify probe configuration
3. Simulate failure (stop health endpoint)
4. Verify pod restarts

## Quick Validation Script

```bash
#!/bin/bash
# Run after migration to validate ShipIt

CLUSTER_ID="your-cluster-id"

echo "Creating test app..."
APP_OUTPUT=$(shipit apps create $CLUSTER_ID \
  --name migration-test \
  --namespace test \
  --image nginx:alpine \
  --replicas 1 \
  --port 80)

APP_ID=$(echo "$APP_OUTPUT" | grep -oP 'id:\s*\K[a-f0-9-]+')

echo "Deploying..."
shipit apps deploy $APP_ID

echo "Waiting for deployment..."
sleep 30

echo "Checking status..."
shipit apps status $APP_ID

echo "Checking logs..."
shipit apps logs $APP_ID --tail 10

echo "Cleaning up..."
shipit apps delete $APP_ID

echo "Test complete!"
```

## Expected Cluster Setup

For test apps to work, the test cluster needs:

1. **Namespace**: `test` namespace created
2. **ECR Access**: Node IAM role with ECR pull permissions
3. **Network**: Pods can communicate within cluster
4. **DNS**: CoreDNS running for service discovery

## Notes

- Test apps use public images (no ECR required)
- Production apps should use private ECR images
- Clean up test apps after validation
