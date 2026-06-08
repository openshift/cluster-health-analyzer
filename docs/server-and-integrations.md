# Server and Integrations

> HTTP server setup, Prometheus metrics exposition, Thanos querier client, Alertmanager API client, authentication/authorization delegation, and TLS configuration. For the core analysis logic that produces the metrics, see [health-analysis-engine.md](health-analysis-engine.md). For deployment manifests and build system, see [build-and-deployment.md](build-and-deployment.md).

## Key Entry Points

- `cmd/serve/serve.go`: `ServeCmd` — Cobra command that parses options, builds the API server, and calls `server.StartServer()`
- `cmd/serve/apiserver.go`: `APIServer` struct — wraps `k8s.io/apiserver/pkg/server.GenericAPIServer` to implement `server.Server` interface. `buildServerConfig()` configures auth delegation, TLS, and cipher suites
- `pkg/server/server.go`: `StartServer()` — orchestrates startup: creates processors (if not disabled), initializes groups collection from history, registers all MetricSets with a Prometheus registry, mounts `/metrics` handler, and starts the server
- `pkg/server/server.go`: `Server` interface — abstraction with `Handle(pattern, handler)` and `Start(ctx)` methods
- `pkg/prom/loader.go`: `Loader` interface — queries Thanos via PromQL (`LoadQuery`, `LoadAlertsRange`, `LoadVectorRange`). MockGen generates `MockPrometheusLoader` for unit tests
- `pkg/prom/client.go`: `NewPrometheusClient()` — creates a Prometheus API client with optional TLS + Bearer token auth from service account
- `pkg/prom/collector.go`: `MetricSet` — custom `prometheus.Collector` implementation that supports batch `Update()` of metrics. Each processing pipeline writes to its own MetricSet
- `pkg/prom/values.go`: `RangeVector`, `Range`, `Matrix` — data types for time-series query results
- `pkg/alertmanager/loader.go`: `Loader` interface — queries Alertmanager v2 API for active and silenced alerts. MockGen generates `MockAlertManagerLoader`
- `pkg/common/options.go`: `Options` struct — all CLI flags and env-var-driven configuration

## Patterns & Conventions

### Server Lifecycle

`StartServer()` in `pkg/server/server.go` is the main entry point after the Cobra command runs:

1. If `--disable-components-health` is not set: loads component config YAML from `defaultComponentsConfigPath` (`/etc/config/components.yaml`), creates `healthProcessor`, starts it
2. If `--disable-incidents` is not set: creates `processor` with Thanos URL and Alertmanager URL, initializes `GroupsCollection` with 4-day lookback (`historyLookback`), starts it
3. Registers 6 `MetricSet` instances with a new `prometheus.Registry`
4. Mounts `promhttp.HandlerFor(reg)` at `/metrics`
5. Calls `server.Start(ctx)` which blocks

Both processors run as concurrent goroutines. Either can be independently disabled via CLI flags.

### Prometheus Metrics Exposition

Metrics are exposed through `prom.MetricSet` — a custom `prometheus.Collector` that holds a `[]Metric` slice protected by `sync.RWMutex`. Processing pipelines call `Update(metrics)` to atomically swap the entire metric set. The `Collect()` method iterates over the current metrics, building `prometheus.Desc` and `prometheus.MustNewConstMetric` per metric dynamically (labels are not known at registration time).

Six MetricSets are registered:
- `cluster_health_components_map` — alert-to-component mapping
- `cluster_health_components` — component metadata and ranking
- `cluster:health:group_severity:count` — incident group severity counts
- `component_health_alert` — per-component health from alerts
- `component_health_object` — per-component health from Kubernetes objects
- `component_health` — aggregated component health

### Thanos Client

`pkg/prom/client.go` creates a Prometheus API client pointing at Thanos querier:
- **HTTP mode**: plain client, no auth (local dev via `make proxy`)
- **HTTPS mode**: reads Bearer token from `/var/run/secrets/kubernetes.io/serviceaccount/token` and CA cert from `/var/run/secrets/kubernetes.io/serviceaccount/service-ca.crt`

The `Loader` interface (`pkg/prom/loader.go`) wraps the Prometheus v1 API:
- `LoadQuery()` — instant query, returns `[]model.LabelSet`
- `LoadAlertsRange()` — range query for `ALERTS{alertstate="firing"}`, returns `RangeVector`
- `LoadVectorRange()` — generic range query, returns `RangeVector`

`RangeVector` is a slice of `Range` (metric + samples + step). The `Expand()` method converts to a dense `Matrix` but is currently unused (kept for possible future use after switching to interval-based alert change detection).

### Alertmanager Client

`pkg/alertmanager/loader.go` wraps the Alertmanager v2 API using `go-openapi/runtime` transport:
- `ActiveAlerts()` — fetches all active, non-inhibited, non-unprocessed alerts
- `ActiveAlertsWithLabels(labels)` — same with label filter (used by `healthProcessor` for component-scoped queries)
- `SilencedAlerts()` — fetches silenced alerts (used by `processor` for silence evaluation)

TLS setup mirrors the Prometheus client: reads service account token and CA cert from the same paths.

### Authentication & TLS

The server uses `k8s.io/apiserver` (`GenericAPIServer`) for auth and TLS:

- **Auth delegation**: `operatorv1alpha1.DelegatedAuthentication` and `DelegatedAuthorization` delegate to the cluster's kube-apiserver. Disabled via `--disable-auth-for-testing`
- **TLS**: configurable via `--tls-cert-file`, `--tls-private-key-file`, `--tls-min-version` (default `VersionTLS12`, supports `VersionTLS13`), `--tls-cipher-suites`
- **Client CA**: intentionally NOT set on `servingInfo.ClientCA` — the CA is read from the `kube-system/extension-apiserver-authentication` ConfigMap instead
- **HTTP/2 disabled**: `serving.ToServerConfig()` is called with `false` for HTTP/2

Default options are populated from environment variables: `REFRESH_INTERVAL` (default 30s), `PROM_URL` (default `http://localhost:9090`), `ALERTMANAGER_URL` (default `http://localhost:9093`).

## Gotchas

- **Service account paths are hardcoded.** Both `pkg/prom/client.go` and `pkg/alertmanager/loader.go` read token from `/var/run/secrets/kubernetes.io/serviceaccount/token` and CA from `/var/run/secrets/kubernetes.io/serviceaccount/service-ca.crt`. These paths only exist inside a Kubernetes pod with a mounted service account.
- **MetricSet labels are dynamic.** `collector.go` creates `prometheus.Desc` per `Collect()` call because the label set varies per metric. This means Prometheus's `Describe()` only returns a single generic descriptor — tools expecting static label schemas may show warnings.
- **`DisableAuthForTesting` generates self-signed certs.** When auth is disabled, `serving.ToServerConfig()` auto-generates self-signed TLS certificates. The `--tls-cert-file`/`--tls-private-key-file` flags are only used when auth is enabled.
- **History lookback is 4 days, not configurable.** `historyLookback` in `server.go` is a constant (4 * 24h). On startup, the processor queries Thanos for this range to rebuild the groups collection. Long outages beyond 4 days will lose group continuity.
- **Both Prometheus and Alertmanager clients share the same TLS pattern** (duplicated `createCertPool()` and `readTokenFromFile()` functions). Changes to TLS handling must be applied in both packages.
- **`RangeVector.Expand()` is dead code.** The dense `Matrix` conversion exists but is unused — the codebase switched to interval-based processing. It's kept explicitly for possible future use.
- **HTTP/2 is disabled** in the server config. This is intentional and passed as `false` to `serving.ToServerConfig()`.

## Dependencies & Context

- **`k8s.io/apiserver`**: provides `GenericAPIServer` with built-in auth delegation, TLS, and lifecycle management. The server registers a single `/metrics` handler on the `NonGoRestfulMux`
- **`openshift/library-go`**: `config/serving.ToServerConfig()` builds the server config from OpenShift-style `HTTPServingInfo`
- **`prometheus/client_golang`**: `api.Client` for Thanos queries, `prometheus.Collector` interface for metrics exposition, `promhttp` for the HTTP handler
- **`prometheus/alertmanager`**: v2 API client types (`client`, `models`) for alert and silence queries
- **`go-openapi/runtime`**: HTTP transport layer for the Alertmanager v2 client
- **`spf13/cobra` + `spf13/pflag`**: CLI framework for the `serve` subcommand and flag parsing
- **Server interface pattern**: `pkg/server/server.go` defines `Server` as an interface (`Handle` + `Start`). `cmd/serve/apiserver.go` implements it with `GenericAPIServer`. This abstraction allows testing with alternative server implementations

## Links

- [health-analysis-engine.md](health-analysis-engine.md) — the analysis logic that produces metrics exposed here
- [build-and-deployment.md](build-and-deployment.md) — deployment manifests (TLS certs, env vars, ServiceMonitor)
- [testing-infrastructure.md](testing-infrastructure.md) — MockGen mocks for `prom.Loader` and `alertmanager.Loader`
- [pkg/server/server.go](../pkg/server/server.go) — server startup orchestration
- [cmd/serve/apiserver.go](../cmd/serve/apiserver.go) — GenericAPIServer wrapper and TLS config
- [pkg/prom/loader.go](../pkg/prom/loader.go) — Thanos query interface
- [pkg/alertmanager/loader.go](../pkg/alertmanager/loader.go) — Alertmanager API client
