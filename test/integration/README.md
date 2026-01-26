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
- Permissions to create resources in `openshift-monitoring` namespace
- PrometheusRule CRD available (standard OpenShift installation)

## Test Prerequisites

Before running tests, ensure Thanos querier is accessible:

```bash
# Start port-forward to Thanos (required for Prometheus client)
make proxy
```

This runs `oc port-forward -n openshift-monitoring svc/thanos-querier 9090:9091` in the background and is required for tests to query cluster metrics.

## Test Lifecycle

Integration tests follow a specific resource lifecycle pattern:

1. **Resource Creation**: Each test creates resources with unique names (timestamp-based suffix)
2. **No Automatic Cleanup**: Resources are intentionally left running after tests for inspection
3. **Pre-Test Cleanup**: Each test's `BeforeEach` removes its own resources from previous runs
4. **Manual Cleanup**: Remove all test resources with:
   ```bash
   oc delete deployment,prometheusrule -l test-type=crashloop -n openshift-monitoring
   oc delete deployment,prometheusrule -l test-type=stress -n openshift-monitoring
   ```

This approach allows you to inspect resources after test completion while preventing accumulation across test runs.

## Configuration

Override defaults via environment variables:

```bash
CHA_IMAGE=quay.io/myorg/cluster-health-analyzer:dev make deploy-integration
CHA_IMAGE=quay.io/myorg/cluster-health-analyzer:dev make test-integration
```
