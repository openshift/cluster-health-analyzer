# Contributing

## Development Setup

1. Install Go 1.25+
2. Login to an OpenShift cluster: `oc login`
3. Start the Thanos proxy: `make proxy`
4. In a separate terminal, run the server: `make run`
5. Access metrics at: `https://localhost:8443/metrics`

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

# Integration tests (requires oc login + make proxy + make deploy-integration)
make test-integration
```

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

## Project Documentation

See [docs/agents/](docs/agents/) for detailed architecture and subsystem documentation.
