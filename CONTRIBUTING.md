# Contributing

## Development Setup

1. Install Go 1.25+
2. Login to an OpenShift cluster: `oc login`
3. Start the Thanos proxy: `make proxy`
4. In a separate terminal, run the server: `make run`
5. Access metrics at: `https://localhost:8443/metrics`

```bash
curl -k https://localhost:8443/metrics
```

If you are logged into an OpenShift cluster with the `$KUBECONFIG` variable pointing
to the appropriate kubectl configuration, you can run the authenticated version
of the service with:

```bash
go run ./main.go serve --kubeconfig $KUBECONFIG
```

Note that since it requires proper authentication and your local machine
does not have client CAs, you will no longer be able to retrieve the metrics locally.

For development without a live cluster, use simulation:

```bash
make simulate
```

## Running Tests

```bash
# Unit tests
make test

# Unit tests with verbose output
make test-verbose

# Linting
make lint
```

### Integration Tests

The integration tests verify the cluster-health-analyzer backend deployed on an OpenShift cluster.
They treat cluster-health-analyzer as a black box: they trigger events in the cluster or inject alerts
into Prometheus, then observe the resulting metrics that cluster-health-analyzer produces.

Prerequisites:
- Log in to the cluster via `oc login`
- Deploy cluster-health-analyzer: `make deploy-integration` (deploys only the backend; the tests handle no additional setup)
- Proxy Prometheus to localhost: `make proxy`

```bash
make test-integration
```

Running integration tests is recommended for all PRs.

### CI

Lint, unit, and integration tests run automatically in CI and must pass before merging.

## Submitting Changes

1. Fork the repo and create a branch from `main`.
2. Make your changes with tests where applicable.
3. Run `make precommit` to ensure linting and tests pass.
4. Open a pull request with a clear description of what changed and why.

## Code Style

- Follow standard Go formatting (`gofmt`)
- Import groups: stdlib, external dependencies, current project
- Use golangci-lint rules defined in project configuration (`make lint`)
- Keep functions focused and testable

## Data Simulation

For development purposes, it's useful to have some data populated in Prometheus.
A sample csv with some data can be found in `./testdata/input.csv`.

Generate sample alerts and corresponding component and incident mappings:

```bash
SCENARIO=./testdata/input.csv make simulate
```

or:

```bash
go run ./main.go simulate --scenario ./testdata/input.csv
```

The CSV file defines the alerts to be generated and has the following format:

| Field      | Description |
|------------|-------------|
| start      | Start offset in minutes |
| end        | End offset in minutes |
| alertname  | Alert name (e.g. `KubePodCrashLooping`) |
| namespace  | Alert namespace (e.g. `openshift-monitoring`) |
| severity   | Alert severity (e.g. `warning`, `critical`) |
| labels     | Optional JSON object with additional alert labels, in the form of `{"key":"value"}` (e.g. `{"component":"node-exporter"}`) |

Example:

```
start,end,alertname,namespace,severity,labels
0,60,Watchdog,openshift-monitoring,warning,
10,40,ClusterOperatorDegraded,openshift-cluster-version,warning,{"name": "machine-config"}
```

If the CSV file is not provided, the script will generate a default set of alerts (see `simulate.go`).

This script generates `cluster-health-analyzer-openmetrics.txt` file. It can be
then turned into tsdb files via `promtool`, that's available as part of prometheus
installation:

```bash
promtool tsdb create-blocks-from openmetrics cluster-health-analyzer-openmetrics.txt
```

Finally, you copy the files to the cluster that's running the Health Analyzer:

```bash
for d in data/*; do
  echo $d
  kubectl cp $d openshift-monitoring/prometheus-k8s-0:/prometheus -c prometheus
done
```

Once complete, the data will appear in the target cluster.

## Project Documentation

See [docs/agents/](docs/agents/) for detailed architecture and subsystem documentation.
