# Cluster Provisioning Plan

This document outlines the plan for provisioning test and additional production clusters for ShipIt.

## Current Infrastructure

| Cluster | Type | Region | Status |
|---------|------|--------|--------|
| unboundsecurity-cluster-nclpwi | Production | us-west-2 | Active |
| gateway-data-cluster | Production | us-west-2 | Active |
| shipit-test | Test | us-west-2 | Planned |

## Test Cluster Provisioning

### Prerequisites

1. AWS CLI configured with admin access
2. eksctl installed (`brew install eksctl` or [eksctl releases](https://github.com/weaveworks/eksctl/releases))
3. kubectl installed

### Create Test Cluster

```bash
# Navigate to infra directory
cd infra

# Create cluster (takes ~15-20 minutes)
eksctl create cluster -f eksctl-test-cluster.yaml

# Verify cluster access
kubectl get nodes
kubectl get namespaces
```

### Test Cluster Configuration

The `eksctl-test-cluster.yaml` defines:

- **Cluster**: `shipit-test` in us-west-2
- **Kubernetes Version**: 1.31
- **Node Group**: 1-2 t3.small nodes
- **IAM OIDC**: Enabled for service accounts
- **Logging**: Disabled (test environment)

### Post-Creation Setup

```bash
# Create test namespace
kubectl create namespace test

# Install metrics-server (for HPA testing)
kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml

# Connect cluster to ShipIt
shipit clusters create <project-id> \
  --name "test-cluster" \
  --kubeconfig ~/.kube/config
```

## Production Cluster Management

### Connecting Existing Clusters

For existing EKS clusters:

```bash
# Update kubeconfig
aws eks update-kubeconfig --name <cluster-name> --region us-west-2

# Register in ShipIt
shipit clusters create <project-id> \
  --name "prod-cluster" \
  --kubeconfig ~/.kube/config
```

### Production Requirements

| Component | Requirement |
|-----------|-------------|
| Node Count | Minimum 3 for HA |
| Node Size | t3.medium or larger |
| Disk Size | 50GB+ |
| IAM OIDC | Required |
| Logging | CloudWatch enabled |

## Cost Estimates

### Test Cluster (shipit-test)

| Resource | Quantity | Hourly | Monthly |
|----------|----------|--------|---------|
| EKS Control Plane | 1 | $0.10 | ~$73 |
| t3.small nodes | 1-2 | $0.021/node | ~$15-30 |
| NAT Gateway | 1 | $0.045 | ~$33 |
| **Total** | | | **~$120-135** |

### Production Cluster

| Resource | Quantity | Hourly | Monthly |
|----------|----------|--------|---------|
| EKS Control Plane | 1 | $0.10 | ~$73 |
| t3.medium nodes | 3-5 | $0.042/node | ~$90-150 |
| NAT Gateway | 1 | $0.045 | ~$33 |
| ALB | 1 | $0.023 | ~$17 |
| **Total** | | | **~$215-275** |

## Cleanup

### Delete Test Cluster

```bash
# Remove from ShipIt first
shipit clusters delete <cluster-id>

# Delete EKS cluster
eksctl delete cluster --name shipit-test --region us-west-2
```

## eksctl Configuration Reference

### Minimal Test Cluster

```yaml
apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig

metadata:
  name: shipit-test
  region: us-west-2
  version: "1.31"

managedNodeGroups:
  - name: test-nodes
    instanceType: t3.small
    desiredCapacity: 1
    minSize: 1
    maxSize: 2
    volumeSize: 20
```

### Production Cluster Template

```yaml
apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig

metadata:
  name: shipit-prod
  region: us-west-2
  version: "1.31"

managedNodeGroups:
  - name: prod-nodes
    instanceType: t3.medium
    desiredCapacity: 3
    minSize: 3
    maxSize: 10
    volumeSize: 50
    iam:
      attachPolicyARNs:
        - arn:aws:iam::aws:policy/AmazonEKSWorkerNodePolicy
        - arn:aws:iam::aws:policy/AmazonEKS_CNI_Policy
        - arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryReadOnly
    labels:
      role: worker
      environment: production

iam:
  withOIDC: true

cloudWatch:
  clusterLogging:
    enableTypes:
      - api
      - audit
      - authenticator
```

### Multi-AZ High Availability

```yaml
managedNodeGroups:
  - name: prod-az-a
    instanceType: t3.medium
    desiredCapacity: 2
    minSize: 1
    maxSize: 5
    availabilityZones: ["us-west-2a"]

  - name: prod-az-b
    instanceType: t3.medium
    desiredCapacity: 2
    minSize: 1
    maxSize: 5
    availabilityZones: ["us-west-2b"]
```

## Troubleshooting

### Cluster Creation Fails

```bash
# Check eksctl version
eksctl version

# View detailed logs
eksctl create cluster -f config.yaml --verbose 4
```

### Node Join Issues

```bash
# Check node status
kubectl get nodes -o wide

# View node logs
kubectl describe node <node-name>

# Check AWS Console for node group issues
```

### IAM/RBAC Issues

```bash
# Verify aws-auth ConfigMap
kubectl get configmap aws-auth -n kube-system -o yaml

# Check caller identity
aws sts get-caller-identity
```

## Next Steps

1. **Create test cluster** - `eksctl create cluster -f eksctl-test-cluster.yaml`
2. **Connect to ShipIt** - Add cluster via CLI
3. **Deploy test app** - Validate functionality
4. **Run migration tests** - Test Porter app migration
5. **Document learnings** - Update this guide

## References

- [eksctl Documentation](https://eksctl.io/)
- [Amazon EKS Best Practices](https://aws.github.io/aws-eks-best-practices/)
- [Kubernetes Version Support](https://docs.aws.amazon.com/eks/latest/userguide/kubernetes-versions.html)
