# Integration Tests

OpenShift integration tests for cluster-health-analyzer (CHA). Run commands from
the repository root. Fixtures are embedded in the test binaries; Ginkgo is a Go
module dependency and needs no global installation.

## Usage

| Command | Purpose | Selection |
| --- | --- | --- |
| `make test-integration` | Standard tests against an existing stack | `!stress&&!stress-simulate&&!long` |
| `make test-stress-simulate` | Simulated-alert stress tests, with Make-provided stress settings | `stress-simulate` |
| `make test-integration-long` | Build, deploy, start proxies and run grouping scenarios | `long`, two-hour suite timeout |

For an existing stack, configure [cluster access](#test-prerequisites) and
[environment variables](#configuration) first. For a disposable test cluster, use
the [self-contained run](#self-contained-long-grouping-run-local-or-ci).

Direct Go/Ginkgo runs have no environment-variable gate: without a label filter,
they include long and stress specs. CI entrypoints must exclude these explicitly
when unwanted; no CI job configuration is changed by this suite.

## Requirements

- Go 1.26+ (see `go.mod`), `oc`, and authenticated OpenShift access (`oc login` or an
  existing `KUBECONFIG`). `oc` uses the current context.
- PrometheusRule CRD (standard on OpenShift) and permission to create, read and
  delete test resources in `openshift-monitoring`, including rules during cleanup.
- A running CHA and Prometheus/Thanos/Alertmanager stack for direct test runs.
  `THANOS_URL` must expose both `ALERTS` and `cluster_health_components_map`.

## Test Prerequisites

For an existing stack, forward Thanos in a separate terminal (`make proxy` is an
optional helper):

```bash
oc -n openshift-monitoring port-forward \
  "$(oc -n openshift-monitoring get pod \
    -l app.kubernetes.io/instance=thanos-querier \
    -o jsonpath='{.items[0].metadata.name}')" \
  9095:9090
```

Choose a free local port to avoid querying the Docker debugging Prometheus on
9090. The examples below use 9095. For authenticated Thanos endpoints, configure
existing authentication without printing credentials or placing them in arguments.

## Configuration

Environment variables work with both Make and direct Go/Ginkgo commands. Defaults
come from [`framework.LoadConfig`](framework/config.go):

| Variable | Default | Purpose |
| --- | --- | --- |
| `THANOS_URL` | `http://localhost:9090` | Metrics backend; deployment settings do not filter queries |
| `NAMESPACE` | `openshift-cluster-health-analyzer` | CHA deployment namespace for standard specs and diagnostics |
| `DEPLOYMENT_NAME` | `cluster-health-analyzer` | CHA deployment name for standard specs and diagnostics |
| `CHA_IMAGE` | Unset | Expected-image diagnostic in direct runs; prebuilt image for the self-contained target |

For example, run the standard suite against the forwarded endpoint:

```bash
THANOS_URL=http://127.0.0.1:9095 make test-integration
```

Equivalent direct invocation, with explicit deployment settings:

```bash
NAMESPACE=openshift-cluster-health-analyzer \
DEPLOYMENT_NAME=cluster-health-analyzer \
THANOS_URL=http://127.0.0.1:9095 \
go run github.com/onsi/ginkgo/v2/ginkgo -v \
  --label-filter='!stress&&!stress-simulate&&!long' ./test/integration/...
```

Grouping specs query the backend selected by `THANOS_URL`; they do not require
deployment settings or perform a deployment preflight check. On failure,
diagnostics attempt to use the configured deployment, returning early if lookup
fails. Standard specs retain both preflight and diagnostics. Only an allow-listed
pod summary is printed; pod logs are never collected. Use `oc logs` manually for
deeper diagnosis.

## Self-contained long grouping run (local or CI)

Requires Bash plus the [requirements](#requirements) above and a disposable test
cluster. The command is identical locally and in Prow:

```bash
make test-integration-long
# Use a prebuilt image instead of building the checkout:
CHA_IMAGE=quay.io/example/cluster-health-analyzer:ci make test-integration-long
```

The target:

1. Installs `yq` if missing and compiles the suite.
2. Unless `CHA_IMAGE` is supplied, uploads checkout build inputs to an OpenShift
   binary Docker build and pushes to the integrated registry. No local container
   engine, registry route or registry login is needed.
3. Injects the image into temporary backend manifests, deploys and waits for rollout.
4. Starts Thanos and Alertmanager port-forwards on allocated loopback ports,
   overriding `THANOS_URL` and `ALERTMANAGER_URL`. Grouping assertions query Thanos;
   Alertmanager is available for diagnostics. Either proxy exiting fails the run.
5. Runs the suite, then removes proxies and temporary files on success, failure or
   interruption, preserving the exit status. **Backend and build/image resources
   remain for diagnostics; repeated runs accumulate build resources.**

Additional requirements and configuration:

- Builds need the OpenShift build API, integrated registry, base-image access and
  Go module downloads. Permissions must cover builds, image streams, backend
  resources (including cluster RBAC), monitoring pod port-forwards and PrometheusRules.
- `NAMESPACE` and `DEPLOYMENT_NAME` must match `MANIFESTS_PATH`.
  `CHA_NAMESPACE`, `CHA_DEPLOYMENT_NAME` and `CHA_MANIFESTS_PATH` override their
  unprefixed equivalents. In Prow (`PROW_JOB_ID` set), the reserved `NAMESPACE` is
  ignored: use `CHA_NAMESPACE`, defaulting to `openshift-cluster-health-analyzer`.
- Existing `KUBECONFIG` is reused; no interactive login or separate proxy command
  is needed. ci-operator can supply `CHA_IMAGE` to skip building.
- Budget for build/rollout plus the two-hour suite and cleanup; the outer test
  timeout is 2h10m.

**This updates the selected backend: never use production or concurrent jobs on
the same backend.** It does not provision OpenShift, reset history or silence
notifications. Prepare [non-notifying routing](#environment-ownership-and-safety).

## Long grouping scenarios against an existing stack

Unlike the self-contained target, direct execution does not build or deploy:

```bash
THANOS_URL=http://127.0.0.1:9095 \
go run github.com/onsi/ginkgo/v2/ginkgo -v \
  --label-filter=long --timeout=2h ./test/integration/tests
```

The five `long`/`grouping` specs run serially in declaration order, with 40 minutes
per spec. A failure skips subsequent cases. To diagnose one case without deployment
settings, replace the filter and timeout with, for example:

```bash
THANOS_URL=http://127.0.0.1:9095 \
go run github.com/onsi/ginkgo/v2/ginkgo -v \
  --label-filter='long && integration-exact-replay' --timeout=40m \
  ./test/integration/tests
```

`grouping && integration-fuzzy-namespace` similarly selects the namespace case.
Grouping specs create **only PrometheusRules in `openshift-monitoring`**: no stack,
exporter, UI, Deployment, namespace, port-forward, storage reset or application
configuration changes.

### Inputs and assertions

[`grouping_scenarios.yaml`](fixtures/testdata/grouping_scenarios.yaml) mirrors the
five debugging UI scenarios: real minute offsets, source labels, durations and
expected groups. Rules use the embedded
[`grouping_prometheusrule.yaml`](fixtures/testdata/grouping_prometheusrule.yaml) template.

| Case | Starts | Expected result |
| --- | --- | --- |
| `integration-time-proximity` | 0m, 10m, 30m | First two alerts share a group; the third starts another. The 15-minute proximity window slides on the second match. |
| `integration-fuzzy-namespace` | 0m, 16m | One group via namespace. |
| `integration-fuzzy-alert-name` | 0m, 16m | One group via alert name across namespaces. |
| `integration-exact-replay` | A: 0–2m, B: 16m, A: 17m | A recovers its original group, distinct from B, with identical source labels. |
| `integration-subset-matching` | A: 0–2m, B: 16m, A: 17m | A recovers its original group after only its severity changes. |

- **Clock:** a constant Unix timestamp captured at render time, before `Apply`.
  Expressions select it within alert windows; its value supplies the stable
  `scenario_start` label. Apply and Operator reconciliation consume the initial
  two-minute window. Prometheus handles 30-second pending, firing and resolution.
- **Identity:** names retain their base plus a unique `Test<run>` suffix, reused
  across windows. Identical-label windows merge into one OR expression; severity
  escalation uses two rules with the same name and different severities. No
  per-window label is added, preserving exact matching.
- **Checks:** `Eventually` polls `GetAlerts`/`GetIncidents` with `BeValidIncident`.
  Tests save group IDs, verify finite alerts and their CHA metrics disappear before
  replay, then compare full partitions, including IDs from resolved windows.
  Assertions cover observable output, not internal matcher distances absent from metrics.
- **Duration:** sequences total **96 minutes**, plus reconciliation, pending,
  scraping and CHA processing. The CHA proximity window remains **15 minutes**.

### Environment ownership and safety

- Use controlled history and avoid unrelated alerts. Unique names and `test_run`
  identify assertions but do not isolate fuzzy namespace or time-proximity matches.
  Serial execution does not clear groups; Analyzer restarts can reload old history.
- Tests neither reset CHA/Prometheus nor delete application data, historical
  Prometheus samples or retained CHA groups.
- These are synthetic aggregation tests, not actual etcd/storage failure or exporter
  tests; they do not deliberately break cluster components.
- Configure non-notifying Alertmanager routing or a silence for
  `test_type="incident-grouping-long"` before running. Otherwise alerts can reach
  existing receivers. Tests never change or delete Alertmanager configuration/data.

### Grouping cleanup

- Before each case, remove previous rules matching both
  `test-type=incident-grouping-long` and that case's `grouping-case` label.
- Before Apply, register `DeferCleanup` to delete only the run's named rule on
  success, failure or timeout. Wait for pending/firing alerts, the recording clock
  and current CHA metrics to disappear; the clock check catches loaded rules that
  could still generate future alerts.
- Allow one minute for rule deletion/disappearance and five for metric
  reconciliation. Cleanup has a fresh context and its own seven-minute timeout,
  independent of spec cancellation. Cleanup failures fail the test.

Cleanup adds time per case; the suite timeout remains two hours. With `go test`,
leave room in the outer timeout: use `-timeout=50m` with `-ginkgo.timeout=40m`, or
`-timeout=2h10m` with a two-hour Ginkgo timeout. An outer timeout or `SIGKILL` can
prevent cleanup. Recover old/interrupted runs manually:

```bash
oc delete prometheusrule -n openshift-monitoring -l test-type=incident-grouping-long
```

## Deployment/crashloop test lifecycle

Unlike grouping specs, these tests leave timestamp-named resources for inspection.
Each test's `BeforeEach` removes its own previous resources. To clean up manually:

```bash
oc delete deployment,prometheusrule -l test-type=crashloop -n openshift-monitoring
oc delete deployment,prometheusrule -l test-type=stress -n openshift-monitoring
```

## Local verification without a cluster

```bash
# Validate fixtures and, if promtool is installed, evaluate all time windows.
go test ./test/integration/fixtures -run TestGroupingRuleFixtures -v

# Check cleanup queries with a simulated loader.
go test ./test/integration/framework -run TestHasGroupingData -v

# Compile without invoking BeforeSuite or contacting a cluster.
go test ./test/integration/... -run '^$'
```

The fixture test does not install promtool. If absent, it reports skipped rule-engine
evaluation; YAML and alert-identity checks still run.
