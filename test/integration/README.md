# Integration Tests

Basic integration test suite for cluster-health-analyzer running on OpenShift.

## Usage (Make is optional)

Provision the existing stack independently and run these commands from the repository
root. Tests do not invoke Make or require Make-generated files or environment flags.
Configuration defaults are defined in `framework.LoadConfig`; fixtures are embedded
in the test binaries. Ginkgo is already a Go module dependency and needs no global
installation.

```bash
# Standard integration tests; excludes stress and long scenarios.
go run github.com/onsi/ginkgo/v2/ginkgo -v \
  --label-filter='!stress&&!stress-simulate&&!long' ./test/integration/...
```

## Long grouping scenarios (manual only)

```bash
go run github.com/onsi/ginkgo/v2/ginkgo -v \
  --label-filter=long --timeout=2h ./test/integration/tests
```

This command selects only the five `long`/`grouping` specs, executes them serially
in declaration order and sets a two-hour suite timeout (40 minutes per spec).
No environment-variable gate exists in the Go tests and no CI job is changed.
Direct Go/Ginkgo runs without a label filter include the long specs, so CI entrypoints
must explicitly exclude `long` when these cases are not intended to run. Stress
specs likewise require explicit exclusion from standard runs.

The Make targets are optional wrappers around the same commands:

- `make test-integration`: selects `!stress&&!stress-simulate&&!long`.
- `make test-stress-simulate`: selects `stress-simulate` with the Make-provided stress settings.
- `make test-integration-long`: selects `long` with a two-hour timeout.
An ordered run stops subsequent cases after a failure; individual cases can also
be selected for diagnosis:

```bash
go run github.com/onsi/ginkgo/v2/ginkgo \
  -v --label-filter='long && integration-exact-replay' --timeout=40m \
  ./test/integration/tests
```

A running CHA and the Prometheus/Thanos/Alertmanager stack must already be configured
by the developer, with alert and CHA metrics queryable through `THANOS_URL`. Grouping
tests do not look up a CHA Deployment or use `NAMESPACE`/`DEPLOYMENT_NAME` to select
an instance: their `grouping` label bypasses deployment preflight and deployment-log
diagnostics. Other specs keep those checks and diagnostics.
The new tests create **only PrometheusRule resources** in
`openshift-monitoring`: no stack, exporter, UI, Deployment, port-forward, namespace,
storage reset or application-configuration changes are introduced.

### Inputs and assertions

`fixtures/testdata/grouping_scenarios.yaml` contains the same five scenarios as the
debugging UI, with their real minute offsets, source labels, durations and expected
groups. `grouping_prometheusrule.yaml` uses the existing embedded-template pattern.

Each rule resource includes a recording rule that captures a scenario clock on its
first evaluation (`... or on() vector(time())`) and retains that value. Alerting
expressions select the clock only inside their configured windows; Prometheus
handles the real 30-second pending, firing and resolution transitions. Keeping the
clock value also supplies a stable `scenario_start` alert label for timing checks.
The clock starts at rule evaluation, not Kubernetes apply time, so Operator
reconciliation does not consume the initial two-minute window.

Alert names retain their recognizable base names but receive a unique `Test<run>`
suffix. Repeated windows use the same suffixed name. Identical-label windows merge
into one OR expression, while severity escalation uses two rules with the same
name and different severity labels. No label identifies an individual window,
which would invalidate the exact-matching case.

| Case | Starts | Expected result |
| --- | --- | --- |
| `integration-time-proximity` | 0m, 14m, 30m | First two alerts share a group; the third starts another. The 15-minute proximity window slides when the second alert matches. |
| `integration-fuzzy-namespace` | 0m, 16m | One group via namespace. |
| `integration-fuzzy-alert-name` | 0m, 16m | One group via alert name across namespaces. |
| `integration-exact-replay` | A: 0–2m, B: 16m, A: 17m | A recovers its original group, distinct from B; all source labels remain identical. |
| `integration-subset-matching` | A: 0–2m, B: 16m, A: 17m | A recovers its original group after only its severity changes. |

The tests reuse `GetAlerts`, `GetIncidents` and `BeValidIncident`, polling with
`Eventually`. They capture group IDs as each window appears, confirm finite alerts
and their CHA metrics disappear before a replay, and compare complete group
partitions, including the saved IDs of resolved windows. The assertions verify
observable output; internal matcher distances are not exposed by CHA metrics.
The sequences alone total **96 minutes**; reconciliation, pending, scraping and
CHA processing add overhead. The CHA time-proximity window remains **15 minutes**.

### Environment ownership and safety

- Supply controlled history and avoid unrelated alerts during each case. Unique
  alert names and `test_run` identify assertions but do not isolate fuzzy namespace
  or time-proximity matching. Serial execution does not clear previous groups.
- An Analyzer restart can reload previous Prometheus history. Tests never delete
  application data or reset CHA/Prometheus to obtain a passing result.
- These are synthetic aggregation tests, not tests of actual etcd/storage failures
  or exporter behavior. They do not deliberately break cluster components.
- Prepare non-notifying Alertmanager routing or a silence for
  `test_type="incident-grouping-long"` before opting in. Otherwise test alerts can
  reach the existing receivers. The tests do not change Alertmanager configuration.

Before each case, cleanup removes only previous rules matching both
`test-type=incident-grouping-long` and that case's `grouping-case` label.
For the current run, `DeferCleanup` is registered before Apply: on success, failure
or timeout it removes only that run's named PrometheusRule, then waits for its
pending/firing alerts, recording clock and current CHA metrics to disappear.
The clock check prevents declaring cleanup complete while a loaded rule could
still generate future alerts. Deletion/disappearance gets one minute, metric
reconciliation five minutes, and the cleanup node has its own seven-minute timeout
and fresh context, independent of a cancelled spec context. Cleanup adds this
reconciliation time to each case; the two-hour suite timeout setting is unchanged.
When using `go test`, leave room for cleanup in its outer timeout: for example,
`-timeout=50m` with `-ginkgo.timeout=40m`, or `-timeout=2h10m` with a two-hour Ginkgo
suite timeout. An outer process timeout can otherwise kill the runner during cleanup.

Historical Prometheus samples and retained CHA groups are not erased, and no
Alertmanager configuration or data is deleted. Cleanup failures fail the test.
A forcibly killed runner (`SIGKILL`) cannot run cleanup; manual recovery remains
available for old or interrupted runs:

```bash
oc delete prometheusrule -n openshift-monitoring -l test-type=incident-grouping-long
```

### Local verification without a cluster

```bash
# Validate rendered fixtures; also exercise all time windows with promtool if installed.
go test ./test/integration/fixtures -run TestGroupingRuleFixtures -v

# Verify cleanup-state queries with a simulated loader.
go test ./test/integration/framework -run TestHasGroupingData -v

# Compile the integration suite without invoking BeforeSuite or contacting a cluster.
go test ./test/integration/... -run '^$'
```

The fixture test does not install promtool. If unavailable, it reports that rule
engine evaluation was skipped; YAML and alert-identity checks still run.

## Requirements

- OpenShift cluster access (`oc login` required)
- Go 1.26+ (as required by `go.mod`)
- Permissions to create resources in `openshift-monitoring` namespace
- PrometheusRule CRD available (standard OpenShift installation)

## Test Prerequisites

Before running tests, ensure `THANOS_URL` points to the cluster's Thanos/Prometheus
and can query both `ALERTS` and `cluster_health_components_map`. For example, forward
the Thanos pod's HTTP port to a free local port in a separate terminal:

```bash
oc -n openshift-monitoring port-forward \
  "$(oc -n openshift-monitoring get pod \
    -l app.kubernetes.io/instance=thanos-querier \
    -o jsonpath='{.items[0].metadata.name}')" \
  9095:9090
```

Set `THANOS_URL=http://127.0.0.1:9095` for the test command below. Choose a free local
port rather than accidentally querying the Docker debugging Prometheus on port 9090.
`make proxy` is an optional helper, not a prerequisite.

## Deployment/crashloop test lifecycle

The grouping tests use the automatic cleanup described above. Existing
deployment/crashloop tests retain their resource lifecycle pattern:

1. **Resource Creation**: Each test creates resources with unique names (timestamp-based suffix)
2. **No Automatic Cleanup (deployment/crashloop only)**: Resources are intentionally left running after tests for inspection
3. **Pre-Test Cleanup**: Each test's `BeforeEach` removes its own resources from previous runs
4. **Manual Cleanup**: Remove all test resources with:
   ```bash
   oc delete deployment,prometheusrule -l test-type=crashloop -n openshift-monitoring
   oc delete deployment,prometheusrule -l test-type=stress -n openshift-monitoring
   ```

This approach allows you to inspect resources after test completion while preventing accumulation across test runs.

## Configuration

Use ordinary environment variables directly, regardless of how the tests are launched:

```bash
NAMESPACE=openshift-cluster-health-analyzer \
DEPLOYMENT_NAME=cluster-health-analyzer \
THANOS_URL=http://127.0.0.1:9095 \
go run github.com/onsi/ginkgo/v2/ginkgo -v \
  --label-filter='!stress&&!stress-simulate&&!long' ./test/integration/...
```

Defaults are `NAMESPACE=openshift-cluster-health-analyzer`,
`DEPLOYMENT_NAME=cluster-health-analyzer` and `THANOS_URL=http://localhost:9090`.
`NAMESPACE` and `DEPLOYMENT_NAME` identify the existing CHA deployment for the
standard deployment/crashloop specs and their diagnostics. They are not required
when running only grouping specs. Test rules are still created in
`openshift-monitoring`. For example, run one grouping case without deployment settings:

```bash
THANOS_URL=http://127.0.0.1:9095 \
go run github.com/onsi/ginkgo/v2/ginkgo -v \
  --label-filter='grouping && integration-fuzzy-namespace' --timeout=40m \
  ./test/integration/tests
```

The URL selects the metrics backend; deployment name/namespace are not query filters.
`oc` uses the current context and normal `KUBECONFIG` mechanism. Configure existing
authentication for authenticated Thanos endpoints; no credentials need to be printed
or placed in command arguments. `CHA_IMAGE` is used only for the standard specs'
expected-image diagnostic, not to deploy an image during tests.
