# Cluster Health Analyzer

An OpenShift cluster health analysis tool that maps Prometheus alerts to high-level components, groups alerts into incidents, evaluates component health, and exposes results as Prometheus metrics.

## Quick Reference

### Building and Testing
- `make build` — compile the binary
- `make test` — run unit tests
- `make lint` — run golangci-lint
- `make precommit` — lint + test (run before submitting PRs)
- `make generate` — run Go code generation (MockGen mocks)

### Running Locally
- `make proxy` — port-forward to thanos-querier (required for `make run`)
- `make run` — start server at `https://localhost:8443/metrics` with auth disabled
- `make simulate` — generate test data from CSV scenarios
- `make run-mcp` — start the MCP server

### Deployment
- `make deploy` / `make undeploy` — deploy/remove from cluster (requires `oc login`)

## Architecture at a Glance

> For the full architecture index, see [ARCHITECTURE.md](ARCHITECTURE.md).

The binary has three subcommands: `serve` (main server), `simulate` (test data generation), and `mcp` (AI integration).

The server runs two concurrent processing pipelines:
1. **Incident processor** (`pkg/processor/`) — queries Thanos for firing alerts, maps them to components, groups into incidents
2. **Health processor** (`pkg/health/`) — queries Alertmanager and Kubernetes API, evaluates component tree health

Both expose results as Prometheus metrics at `/metrics`.

## Documentation

See [ARCHITECTURE.md](ARCHITECTURE.md) for the full documentation index.