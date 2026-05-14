# Testing Infrastructure

> Covers the test pyramid: unit tests (Go standard + testify), Ginkgo/Gomega integration tests against a live OpenShift cluster, stress tests via simulated alert injection, and the `simulate` CLI for generating OpenMetrics test data. For the core health analysis logic under test, see [health-analysis-engine.md](health-analysis-engine.md). For build commands, see [build-and-deployment.md](build-and-deployment.md).

## Key Entry Points

- `make test` / `make test-verbose`: run unit tests (excludes `test/integration/`); defined in [Makefile](../../Makefile)
- `make test-integration`: run integration tests via Ginkgo against a live cluster; defined in [test.mk](../../test.mk)
- `make test-stress-simulate`: run stress tests with simulated alert injection; defined in [test.mk](../../test.mk)
- `cmd/simulate/simulate.go`: CLI entry point for the `simulate` subcommand — generates OpenMetrics `.txt` files from CSV scenarios
- `pkg/simulate/simulate.go`: `Simulate()` function — core simulation logic that converts `RelativeInterval` definitions to OpenMetrics output
- `pkg/simulate/intervals.go`: CSV parsing and default alert scenario definitions
- `pkg/test/mocks/`: MockGen-generated mocks for `prom.Loader` and `alertmanager.Loader` interfaces
- `pkg/utils/fixtures.go`: `RelativeInterval` type and converters for building test time series
- `pkg/utils/helpers.go`: Generic `Ptr[T]()` helper used across tests
- `test/integration/tests/suite_test.go`: Ginkgo suite entry point with `BeforeSuite` cluster validation
- `test/integration/framework/`: Cluster interaction, Prometheus client, matchers, config, env helpers
- `test/integration/simulate/`: Alert injection pipeline (scenario builder → simulate → TSDB blocks → `oc cp` to Prometheus pods)
- `test/integration/fixtures/`: Embedded YAML templates for test resources (Deployments, PrometheusRules)
- `testdata/input.csv`: Default CSV scenario with 19 alerts covering typical OpenShift failure patterns

## Patterns & Conventions

### Unit Tests

Unit tests use standard Go `*_test.go` convention with `testify/assert` for assertions. External dependencies (Prometheus, Alertmanager) are mocked using `go.uber.org/mock` (MockGen). Mocks are generated into `pkg/test/mocks/` via `go generate` directives on the source interfaces (e.g., `pkg/prom/loader.go` → `pkg/test/mocks/mock_prometheus_loader.go`).

The `RelativeInterval` type in `pkg/utils/fixtures.go` is the standard building block for test data — it defines alert time windows as relative minute offsets. `RelativeIntervalsToRangeVectors()` converts them to `prom.RangeVector` for unit tests; `RelativeToAbsoluteIntervals()` in `pkg/simulate/intervals.go` converts them to `processor.Interval` for simulation.

### Integration Tests

Integration tests use Ginkgo v2 + Gomega and run against a live OpenShift cluster. The test suite lives in `test/integration/tests/` with a shared `BeforeSuite` that validates the deployment exists before running any specs.

The `framework.Cluster` type wraps `oc` CLI commands (not client-go) for all cluster interactions: applying YAML, checking deployment status, port-forwarding, pod inspection. `framework.PrometheusClient` wraps `pkg/prom.Loader` to query alerts and incidents from Thanos.

Custom Gomega matcher `BeValidIncident()` validates that processed incidents have all required labels (`src_alertname`, `src_severity`, `src_namespace`, `component`, `layer`, `group_id`) and that `component` and `layer` values are from the known OpenShift set defined in `framework/matchers.go`.

Test resource fixtures use Go `embed` to bundle YAML templates (`test/integration/fixtures/testdata/*.yaml`) that are rendered with `text/template` at runtime. Each test generates unique resource names with timestamp suffixes to avoid conflicts across runs.

Test lifecycle: resources are intentionally left running after tests for inspection. Each test's `BeforeEach` cleans up resources from previous runs using label selectors (e.g., `test-type=crashloop`).

Configuration is driven by environment variables loaded via `framework.LoadConfig()`: `CHA_IMAGE`, `MANIFESTS_PATH`, `DEPLOYMENT_NAME`, `NAMESPACE`, `THANOS_URL`, `THANOS_TOKEN`. Defaults target a standalone CHA deployment; use `eval $(make env-coo)` for COO production layout.

### Stress Tests

Stress tests (`test/integration/tests/stress_simulate_test.go`, labeled `stress-simulate`) inject large numbers of simulated alerts directly into Prometheus TSDB and verify the analyzer processes them correctly.

The injection pipeline (`test/integration/simulate/`):
1. `ScenarioBuilder` creates a CSV scenario programmatically (e.g., `AddStressAlerts(500, prefix, ns, 3000, 4000)`)
2. `Injector.Inject()` runs `pkg/simulate.Simulate()` with `--alerts-only` to generate OpenMetrics
3. `promtool tsdb create-blocks-from openmetrics` converts to TSDB blocks
4. `oc cp` copies blocks into Prometheus pod(s) with automatic fallback (`prometheus-k8s-0` → `prometheus-k8s-1`)

Configurable via env vars: `STRESS_ALERT_COUNT` (required), `STRESS_ALERT_TIMEOUT_MIN` (default 6), `STRESS_INCIDENT_TIMEOUT_MIN` (default 10), `KEEP_TEST_DATA` (skip Prometheus wipe).

### Simulation Engine

The `simulate` CLI subcommand generates OpenMetrics text files that can be loaded into Prometheus for development without a live cluster. Two modes:
- **Default scenario**: hardcoded in `pkg/simulate/intervals.go` — 19 `RelativeInterval` entries modeling a typical cluster degradation (watchdog, node failures, operator degradation, daemonset issues)
- **CSV scenario**: pass `--scenario <file>` to load from CSV. Format: `start,end,alertname,namespace,severity,silenced,labels` where start/end are relative minutes and labels is optional JSON

The `--alerts-only` flag skips generating `cluster_health_components` and `cluster_health_components_map` metrics — used by integration tests so the analyzer-under-test computes those itself.

The simulation adds a 10-minute `endTimeBuffer` to alert end times to prevent Prometheus staleness from hiding alerts during delayed queries.

## Gotchas

- **Mocks are generated code.** `pkg/test/mocks/mock_*.go` are marked `DO NOT EDIT`. Regenerate with `make generate` after changing `prom.Loader` or `alertmanager.Loader` interfaces.
- **Integration tests require `oc login` + `make proxy`.** The Prometheus client connects to Thanos via port-forward (`localhost:9090`). Without the proxy, all Prometheus queries fail.
- **Integration tests leave resources running.** Cleanup is manual: `oc delete deployment,prometheusrule -l test-type=crashloop -n openshift-monitoring`. `BeforeEach` handles cleanup from previous runs automatically.
- **Stress tests wipe Prometheus data by default.** `Injector.WipePrometheusData()` deletes ALL data from `/prometheus/` on the StatefulSet pods and restarts them. Set `KEEP_TEST_DATA=true` to skip. Only run on dedicated test clusters.
- **endTimeBuffer is critical for alert visibility.** Without the 10-minute buffer in `pkg/simulate/intervals.go:335`, alerts end exactly at `time.Now()` and start going stale in Prometheus's 5-minute staleness window, causing tests to see only ~80% of injected alerts.
- **ValidComponents/ValidLayers in matchers.go must stay in sync** with `pkg/processor/alerts.go` layer definitions and `pkg/processor/mappings.go` matcher definitions. If a new component is added to the processor, the test matcher needs updating or incident validation will fail.
- **CSV format is strict**: exactly 7 fields per row, header row skipped, labels field is JSON or empty string.

## Dependencies & Context

- **Ginkgo v2 + Gomega**: integration test framework. Chosen for BDD-style specs and `Eventually` polling — essential for waiting on async cluster state (alert firing, incident processing).
- **testify/assert**: used for unit test assertions (simpler than Gomega for synchronous checks).
- **go.uber.org/mock (MockGen)**: generates interface mocks for `prom.Loader` and `alertmanager.Loader`.
- **promtool**: Prometheus CLI tool used to convert OpenMetrics text to TSDB blocks. Installed via `hack/install-promtool.sh`. Required for stress test injection pipeline.
- **oc CLI**: all cluster interactions in integration tests shell out to `oc` rather than using client-go. This is intentional — the project targets OpenShift specifically (not vanilla Kubernetes), so `oc` provides the right abstraction level and avoids pulling in client-go complexity for test-only cluster access. The `framework.Cluster` type is a thin wrapper around `oc` commands.
- The simulation engine reuses `pkg/processor` types (`Interval`, `GroupsCollection`, `MapAlerts`, `BuildComponentRanks`) to generate realistic processed metrics, ensuring test data matches production output format.

## Links

- [health-analysis-engine.md](health-analysis-engine.md) — the core analysis logic that tests verify
- [build-and-deployment.md](build-and-deployment.md) — build commands including `make test`, `make deploy-integration`
- [test/integration/README.md](../../test/integration/README.md) — integration test usage guide
- [development.md](../../development.md) — CSV format details for simulation scenarios
- [testdata/input.csv](../../testdata/input.csv) — default simulation scenario
