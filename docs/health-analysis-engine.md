# Health Analysis Engine

> Core analysis logic: maps Prometheus alerts to OpenShift components, groups alerts into incidents, evaluates component health via kube-health, and exposes results as Prometheus metrics. For the HTTP server that hosts this engine, see [server-and-integrations.md](server-and-integrations.md). For testing approaches, see [testing-infrastructure.md](testing-infrastructure.md).

## Key Entry Points

- `pkg/processor/processor.go`: `processor` struct — the main processing loop. Loads firing alerts from Prometheus, maps them to components, assigns incident group IDs, evaluates silences, and updates metric sets on each iteration
- `pkg/processor/alerts.go`: `MapAlerts()` — maps `[]model.LabelSet` to `[]ComponentHealthMap` by running alerts through the matcher chain: `cvoAlertsMatcher` → `computeMatcher` → `coreMatcher` → `workloadMatcher`
- `pkg/processor/mappings.go`: `coreMatchers` and `workloadMatchers` — the static mapping tables that define which OpenShift namespaces/alert names belong to which components
- `pkg/processor/incidents.go`: `GroupsCollection` — incident detection engine. Groups alerts by time proximity and label similarity, assigns UUID-based `group_id` labels
- `pkg/processor/types.go`: `ComponentHealthMap` struct — the central data type representing a mapped alert with layer, component, source labels, group ID, health value, and silence status
- `pkg/health/health_processor.go`: `healthProcessor` — the secondary processor that evaluates component health from a YAML-defined component tree, using Alertmanager alerts and kube-health object status checks
- `pkg/health/alert_matcher.go`: `alertMatcher` — matches active Alertmanager alerts against component selector configurations
- `pkg/health/kube_health_checker.go`: `kubeHealthChecker` — wraps `rhobs/kube-health` to evaluate Kubernetes object health (nodes, clusteroperators, machineconfigpools, kubevirts)
- `pkg/health/types.go`: `ComponentsConfig`, `Component`, `K8sObject` — YAML-driven configuration types for the component health tree
- `pkg/common/labels.go`: `LabelsMatcher` interface and implementations (`LabelsSubsetMatcher`, `LabelsIntersectionMatcher`, `labelMatcher`) — the matching primitives used throughout

## Patterns & Conventions

### Alert-to-Component Mapping

Alerts are mapped to components via a chain of `componentMatcherFn` functions evaluated in priority order in `determineComponent()`:

1. **CVO alerts**: `ClusterOperatorDown` and `ClusterOperatorDegraded` are matched by alertname and mapped to the component named in their `name` label (falling back to `"version"`)
2. **Compute alerts**: a hardcoded list of ~25 node-related alertnames (e.g., `KubeNodeNotReady`, `NodeCpuHigh`, MCO alerts) mapped to `compute/compute`
3. **Core matchers**: 28 namespace-based matchers mapping `openshift-*` namespaces to core components (etcd, kube-apiserver, monitoring, network, storage, etc.). Some also match by alertname (e.g., `machine-config` matches both its namespace and specific alert names)
4. **Workload matchers**: ~10 matchers for optional workloads (kubevirt, logging, compliance, gitops, quay, Argo). Kubevirt matches on `kubernetes_operator_part_of` label; Argo matches alertname by regex `^Argo`

Unmatched alerts fall through to `"Others"/"Others"`. Component ranks are assigned in `BuildComponentRanks()`: compute=1, core=10+5*i, workload=1000+5*i (ordered by position in the matcher arrays).

### Incident Grouping

The `GroupsCollection` groups alerts into incidents based on time proximity and label similarity. Each alert gets a `group_id` (UUID) label.

**Matching strategy** (hierarchical, from tightest to fuzziest):
- **Distance 0 (exact)**: all labels match — direct re-identification of the same alert across processing iterations
- **Distance 1 (subset)**: matches on `namespace`, `alertname`, `service`, `job`, `container`
- **Distance 2 (fuzzy)**: matches on individual `alertname` or `namespace` labels separately. Certain alerts are excluded from fuzzy matching: `Watchdog` and `AlertmanagerReceiversNotConfigured`

**Time windows** for matching:
- Fuzzy matches (`distance >= 1`): up to 24 hours
- Direct matches (`distance == 0`): up to 5 days
- Pure time-based grouping (no label match): up to 15 minutes

**Watchdog handling**: if a Watchdog alert appears in a batch, no shared root group is created — this prevents unrelated alerts from being grouped together just because they started at the same time (e.g., after a restart or data outage).

**Group ID preservation across restarts**: `UpdateGroupUUIDs()` loads previous `cluster_health_components_map` metrics and matches current groups against them using `previousIncidentsMatcher`, reassigning UUIDs so incidents keep their identity.

**Group pruning**: `PruneGroups()` removes stale groups — direct matches after 5 days, fuzzy matches after 24 hours.

### Component Health Evaluation

The `healthProcessor` (separate from the main `processor`) evaluates health from a YAML-defined component tree (`ComponentsConfig`):

1. **Tree construction**: the YAML config defines hierarchical components (e.g., `control-plane` → `nodes`, `capacity`, `operators`). At startup, `finalizeComponentTree()` dynamically adds all ClusterOperator names from the cluster API as children of `control-plane.operators`
2. **Alert evaluation**: `alertMatcher` queries Alertmanager for active alerts matching each component's selector config. Selectors support multi-value OR (values within one label key) and multi-label AND
3. **Object health**: `kubeHealthChecker` wraps `rhobs/kube-health` to evaluate Kubernetes objects (nodes, machineconfigpools, kubevirts, clusteroperators) and map their status to `OK`/`Warning`/`Error`/`Unknown`
4. **Health rollup**: `calculateHealthStatus()` recursively aggregates — worst child status wins, then worst alert severity, then worst object status

### Label Matching

`pkg/common/labels.go` provides the matching primitives:

- **`LabelsSubsetMatcher`**: checks that all of its labels exist in the target with matching values (subset match)
- **`LabelsIntersectionMatcher`**: checks that the intersection of label keys has matching values (used for silence evaluation)
- **`labelMatcher`**: matches a single label key against a `ValueMatcher` (string list or regex)

Source labels on exported metrics are prefixed with `src_` (e.g., `src_alertname`, `src_namespace`) to distinguish them from component metadata labels.

## Gotchas

- **Matcher order matters.** The `coreMatchers` and `workloadMatchers` arrays in `mappings.go` are position-ordered — position determines component rank via `BuildComponentRanks()`. Adding a component at the wrong position changes all subsequent ranks.
- **Unrecognized severity defaults to Warning.** `ParseHealthValue()` treats any unknown string as `Warning`, not as healthy. This is intentional but can mask typos in severity labels.
- **Watchdog alerts prevent group creation.** If a batch contains a Watchdog alert, alerts in that batch don't get a shared root group. This is by design (prevents restart-induced false grouping) but means Watchdog alerts always get their own isolated group.
- **Silence evaluation uses intersection matching.** `isAlertSilenced()` uses `LabelsIntersectionMatcher` — a silence on `{alertname=X, namespace=Y}` silences `{alertname=X, namespace=Y, severity=Z}` because the intersection matches, even though the alert has extra labels.
- **`processor` and `healthProcessor` are two separate processing loops** that run concurrently. The `processor` (in `pkg/processor/`) handles alert-to-component mapping and incident grouping. The `healthProcessor` (in `pkg/health/`) handles YAML-configured component tree evaluation. Both can be independently disabled via `--disable-incidents` and `--disable-components-health` flags.
- **Exponential backoff on processing errors.** The main `processor.Run()` uses `wait.ExponentialBackoffWithContext` with `Steps: 4` (1 initial attempt + 3 retries at 1s/1.5s/2.25s delays) before resuming the periodic interval.

## Dependencies & Context

- **`rhobs/kube-health`**: external library for evaluating Kubernetes object health status. Wraps the API server to check conditions on nodes, operators, and custom resources
- **`prometheus/common/model`**: `LabelSet` is the universal alert/metric representation — all matching, mapping, and grouping operates on label sets
- **`prometheus/alertmanager`**: `a../models.Alert` type used for silence evaluation. The loader queries Alertmanager's API for active and silenced alerts
- **`google/uuid`**: generates unique group IDs for incidents
- **Three-layer component model**: compute (node-level), core (OpenShift platform operators), workload (optional add-ons). This is hardcoded in the matcher structure and reflected in metric labels
- **Two processing pipelines**: the `processor` runs PromQL queries against Thanos for alert-based mapping; the `healthProcessor` queries Alertmanager and Kubernetes API for component-tree evaluation. Both expose their results as Prometheus MetricSets

## Links

- [server-and-integrations.md](server-and-integrations.md) — HTTP server, Prometheus metrics exposition, Thanos/Alertmanager integration
- [testing-infrastructure.md](testing-infrastructure.md) — unit test mocks for Loader interfaces, simulation engine
- [build-and-deployment.md](build-and-deployment.md) — components-config ConfigMap (manifests/backend/03_configmap.yaml) that configures the health processor
- [pkg/processor/mappings.go](../pkg/processor/mappings.go) — definitive list of component matchers
- [pkg/processor/incidents.go](../pkg/processor/incidents.go) — incident grouping algorithm
- [pkg/health/health_processor.go](../pkg/health/health_processor.go) — component health evaluation
