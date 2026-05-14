# Cluster Health Analyzer

An analyzer for OpenShift cluster health data.

## Overview

The Cluster Health Analyzer processes the incoming stream of health signals from the
OpenShift cluster and enriches them to provide better views of the data to enhance the
troubleshooting experience.

It provides:

- **Incidents detection**: heuristics to group individual alerts together to allow
  better reasoning about the root cause of the issues.
- **Components mapping and ranking**: an opinionated way to assign the alerts to high-level
  components and rank them based on the importance of the components from the overall cluster
  health perspective.

## Install

Login to a cluster using `oc login` command:

```
oc apply -f manifests/backend
```

## Usage

The Cluster Health Analyzer is a backend that exposes results via two Prometheus metrics:

- `cluster_health_components_map` — maps source signals (alerts) to components and incident groups
- `cluster_health_components` — metadata and ranking of components in the system

See the metrics at `/metrics` on the running service.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, testing, and contribution guidelines.

## Documentation

See [docs/agents/](docs/agents/) for detailed architecture and subsystem documentation.
