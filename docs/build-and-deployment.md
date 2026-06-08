# Build and Deployment

> Covers build system (Makefile), container images (Dockerfile, Dockerfile.konflux), OpenShift deployment manifests (manifests/backend/, manifests/mcp/), and development helper scripts (hack/). For testing commands, see [testing-infrastructure.md](testing-infrastructure.md). For server runtime behavior, see [server-and-integrations.md](server-and-integrations.md).

## Key Entry Points

- [Makefile](../Makefile): primary build orchestration — `build`, `test`, `lint`, `run`, `deploy`, `precommit`
- [test.mk](../test.mk): integration test targets (included by Makefile) — `test-integration`, `test-stress-simulate`, `deploy-integration`
- [Dockerfile](../Dockerfile): multi-stage build for upstream/development images
- [Dockerfile.konflux](../Dockerfile.konflux): multi-stage build for Red Hat Konflux CI pipeline (uses `brew.registry.redhat.io` builder)
- `manifests/backend/`: numbered YAML manifests for the main analyzer deployment (01-07)
- `manifests/mcp/`: YAML manifests for the MCP server deployment
- `hack/listen-thanos.sh`: port-forward to thanos-querier pod on port 9090
- `hack/listen-alertmanager.sh`: port-forward to alertmanager pod on port 9093
- `hack/deploy-integration.sh`: patches manifests with `CHA_IMAGE` and deploys to cluster
- `hack/install-golangci-lint.sh`, `hack/install-promtool.sh`, `hack/install-yq.sh`: tool installers

## Patterns & Conventions

### Makefile Targets

The Makefile follows a convention of short, composable targets:

- **`make build`**: compiles to `bin/cluster-health-analyzer`
- **`make run`**: runs `go run ./main.go serve --disable-auth-for-testing` — requires `make proxy` running in another terminal
- **`make run-mcp`**: runs `go run ./main.go mcp`
- **`make proxy`**: calls `hack/listen-thanos.sh` to port-forward thanos-querier
- **`make lint`**: runs `golangci-lint` (auto-installs via `hack/install-golangci-lint.sh` if missing)
- **`make generate`**: runs `go generate ./...` (produces MockGen mocks)
- **`make simulate`**: runs the simulate subcommand, optionally with `SCENARIO=<csv>` variable
- **`make precommit`**: runs `lint` then `test` — the required check before submitting PRs
- **`make deploy` / `make undeploy`**: applies or deletes `manifests/backend` + `manifests/frontend` via `oc`

Integration targets are in `test.mk` and are documented in [testing-infrastructure.md](testing-infrastructure.md).

### Container Builds

Two Dockerfiles exist with near-identical structure (multi-stage, CGO_ENABLED=1, `-tags strictfipsruntime`):

| File | Builder Base | Purpose |
|------|-------------|---------|
| `Dockerfile` | `golang:1.25` | Upstream/development builds — includes `-tags strictfipsruntime` build flag but does not set `GOEXPERIMENT` |
| `Dockerfile.konflux` | `brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_9_1.25` | Red Hat Konflux internal build pipeline — requires FIPS (sets `GOEXPERIMENT=strictfipsruntime`) |

Both produce a `ubi9/ubi-minimal` runtime image running as non-root user `65532:65532`. The entrypoint is `/bin/cluster-health-analyzer`. The generic Dockerfile uses the standard Go builder and does not require FIPS compliance; the Konflux Dockerfile is for the internal Red Hat build pipeline and requires it.

### Kubernetes Manifests

Backend manifests (`manifests/backend/`) are numbered for apply ordering:
1. **01_namespace.yaml**: creates `openshift-cluster-health-analyzer` namespace with `openshift.io/cluster-monitoring: "true"` label (required for cluster-monitoring-operator scraping)
2. **02_service_account.yaml**: creates SA with `cluster-monitoring-view`, `system:auth-delegator`, and custom `cluster-alerts-view` ClusterRole bindings — grants access to Thanos metrics, Alertmanager API, nodes, clusteroperators, kubevirts, machineconfigpools
3. **03_configmap.yaml**: `components-config` ConfigMap with `components.yaml` — external configuration for `kube-health` component tree (control-plane nodes, capacity alerts, operators, addons like kubevirt)
4. **04_prometheus_rbac.yaml**: Role/RoleBinding for `prometheus-k8s` to scrape the analyzer's namespace
5. **05_deployment.yaml**: single-replica Deployment with TLS cert mounts, PROM_URL and ALERTMANAGER_URL env vars, port 8443
6. **06_service_monitor.yaml**: ServiceMonitor scraping metrics every 30s over mTLS
7. **07_service.yaml**: ClusterIP Service on port 8443 with `serving-cert-secret-name` annotation for auto-provisioned TLS

MCP manifests (`manifests/mcp/`) deploy a separate pod in `openshift-cluster-observability-operator` namespace, listening on port 8085, using the `mcp` subcommand.

## Gotchas

- **`make deploy` references `manifests/frontend`** which does not exist in the repo. This will produce an error if `manifests/frontend` hasn't been created separately.
- **TLS is mandatory in production.** The deployment mounts a serving cert secret (`cluster-health-analyzer-tls`) auto-provisioned by the OpenShift service-ca operator via the Service annotation. Local dev uses `--disable-auth-for-testing` which generates self-signed certs.
- **The namespace label `openshift.io/cluster-monitoring: "true"` is required.** Without it, the cluster-monitoring-operator won't discover the ServiceMonitor and metrics won't be scraped.
- **CGO_ENABLED=1 in Dockerfiles.** The build requires CGO (likely for FIPS compliance via `strictfipsruntime`). This means cross-compilation needs the target platform's C toolchain.
- **Konflux vs upstream Dockerfiles diverge on GOEXPERIMENT.** `Dockerfile.konflux` sets `GOEXPERIMENT=strictfipsruntime` as an env var in addition to the build tag. If the two Dockerfiles get out of sync, FIPS behavior may differ.
- **`hack/deploy-integration.sh` requires `yq`** in addition to `oc`. It patches the deployment image at deploy time using `yq eval -i`.

## Dependencies & Context

- **Go 1.25** with `CGO_ENABLED=1`. The Konflux build requires FIPS compliance via `-tags strictfipsruntime` and `GOEXPERIMENT=strictfipsruntime`; the upstream Dockerfile does not require FIPS
- **golangci-lint**: linting, auto-installed by `hack/install-golangci-lint.sh`
- **promtool**: Prometheus CLI tool for TSDB block creation (used by stress tests, installed via `hack/install-promtool.sh`)
- **yq**: YAML processor for manifest patching during integration deployment
- **oc CLI**: all deployment and cluster operations use the OpenShift CLI
- **OpenShift service-ca operator**: automatically provisions TLS serving certificates from the `serving-cert-secret-name` annotation on Services
- **Konflux CI**: Red Hat's CI/CD system uses `Dockerfile.konflux` with a Red Hat-specific builder image from `brew.registry.redhat.io`

## Links

- [testing-infrastructure.md](testing-infrastructure.md) — integration test commands and test.mk details
- [server-and-integrations.md](server-and-integrations.md) — server runtime, TLS handling, Prometheus/Alertmanager integration
- [manifests/backend/](../manifests/backend/) — backend deployment manifests
- [manifests/mcp/](../manifests/mcp/) — MCP server deployment manifests
- [hack/](../hack/) — development helper scripts
