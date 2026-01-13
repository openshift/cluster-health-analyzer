# Integration Tests

Basic integration test suite for cluster-health-analyzer running on OpenShift.

## Usage

```bash
# Deploy and test
make deploy-integration
make test-integration

# Cleanup
make undeploy-integration
```

## Requirements

- OpenShift cluster access (`oc login` required)
- Go 1.22+

## Configuration

Override defaults via environment variables:

```bash
CHA_IMAGE=quay.io/myorg/cluster-health-analyzer:dev make deploy-integration
CHA_IMAGE=quay.io/myorg/cluster-health-analyzer:dev make test-integration
```
