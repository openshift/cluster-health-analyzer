# AGENTS.md

This file provides guidance to AI coding assistants (Claude Code, GitHub Copilot, etc.) when working with code in this repository.

## Common Development Commands

### Building and Testing
- `make build` - Compiles the cluster-health-analyzer binary
- `make test` - Runs unit tests
- `make test-verbose` - Runs unit tests with verbose output
- `make precommit` - Execute linting and testing (run before submitting PRs)

### Linting and Code Quality
- `make lint` - Run golangci-lint with project configuration
- `make generate` - Run Go code generation

### Running Locally
- `make proxy` - Port-forward to thanos-querier for local development
- `make run` - Execute cluster-health-analyzer server with disabled authentication
  - Listens on `https://localhost:8443/metrics`
  - Requires proxy to be running for Thanos access
- `make run-mcp` - Run the MCP (Model Context Protocol) server locally
- `make simulate` - Generate test data and create metrics file from CSV

### Container and Deployment
- `make deploy` - Deploy services to a cluster (requires `oc login`)
- `make undeploy` - Remove services from the cluster

## CLI Commands

The cluster-health-analyzer binary has three main subcommands:

### 1. serve
Main server mode that analyzes cluster health in real-time:
- Connects to Thanos querier to fetch cluster alerts
- Processes alerts into incident groups
- Maps alerts to high-level components
- Exposes Prometheus metrics at `/metrics` endpoint
- Authentication can be disabled for testing with `--disable-auth-for-testing` flag

### 2. simulate
Development tool for generating test data:
- Reads alert definitions from CSV file
- Creates simulated Prometheus metrics
- Useful for testing without live cluster
- See `development.md` for CSV format details

### 3. mcp
Model Context Protocol server for AI integration:
- Provides structured interface for AI assistants
- Exposes cluster health analysis capabilities

## Project Architecture

### Package Structure

**cmd/** - CLI commands and entry points
- `cmd/serve/` - Main server implementation
- `cmd/simulate/` - Test data generation
- `cmd/mcp/` - MCP server implementation

**pkg/health/** - Core health analysis logic
- Alert processing and grouping (incident detection)
- Component mapping and ranking
- Kubernetes health checking
- Alert matching rules

**pkg/processor/** - Data processing pipeline
- Transforms raw alerts into structured health data
- Applies heuristics for root cause analysis

**pkg/prom/** - Prometheus integration
- Metrics exposition
- Thanos querier client
- Alert fetching and parsing

**pkg/alertmanager/** - Alertmanager integration
- Alert silence detection and handling

**pkg/server/** - HTTP server and API
- Metrics endpoint
- Authentication handling
- TLS configuration (self-signed certificates for local dev)

**pkg/mcp/** - Model Context Protocol implementation
- AI assistant integration layer

**pkg/common/** - Shared utilities
- Label processing
- Common constants and types

**pkg/utils/** - General utilities
- Helper functions
- Testing utilities

**pkg/test/** - Test helpers and fixtures
- Shared test data
- Mock implementations

### Key Metrics Exposed

The analyzer exposes two main Prometheus metrics:

1. **cluster_health_components_map**
   - Maps individual alerts to components
   - Labels include: alert name, namespace, severity, silencing status, incident group ID

2. **cluster_health_components**
   - Provides component metadata and ranking
   - Shows component layer (e.g., "compute", "storage")
   - Includes importance ranking for prioritization

## Development Workflow

### Before Submitting Changes
Always run before creating pull requests:
```bash
make precommit  # Runs linting and tests
```

### Testing Pattern
- Test files follow Go convention: `*_test.go`
- Use table-driven tests where appropriate
- Mock external dependencies (Kubernetes, Prometheus) for unit tests
- Test utilities available in `pkg/test/` and `pkg/utils/`

### Local Development Setup
1. Login to OpenShift cluster: `oc login`
2. Start Thanos proxy: `make proxy`
3. In separate terminal, run server: `make run`
4. Access metrics at: `https://localhost:8443/metrics`

### Simulating Test Data
For development without a live cluster:
1. Create CSV file with alert definitions (see `development.md`)
2. Run: `make simulate`
3. Use `promtool` to convert metrics to TSDB
4. Copy to cluster Prometheus with `kubectl cp`

## Configuration

### Authentication
- Production: Uses Kubernetes service account via `$KUBECONFIG`
- Testing: Disable with `--disable-auth-for-testing` flag

### Thanos Connection
- Server expects Thanos querier available via port-forward
- Default: `http://localhost:9090` (configured via proxy)
- Queries alerts using PromQL

### TLS Certificates
- Local development uses self-signed certificates
- Metrics endpoint always served over HTTPS
- Certificate handling in `pkg/server/`

## Go Module Configuration
- Go version: 1.24
- Main dependencies:
  - Kubernetes client-go and apimachinery
  - Prometheus client_golang and alertmanager
  - OpenShift API libraries
  - Cobra for CLI framework
- Run `go mod tidy` and `go mod vendor` to update dependencies

## Code Style
- Follow standard Go formatting (`gofmt`)
- Import groups: stdlib, external dependencies, current project
- Use golangci-lint rules defined in project configuration
- Keep functions focused and testable
