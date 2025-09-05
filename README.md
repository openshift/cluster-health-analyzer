# Cluster Health Analyzer

An analyzer for OpenShift cluster health data.

## Overview

The Cluster Health Analyzer processes the incoming stream of health signals from the 
OpenShift cluster and enriches them to provide better views of the data to enhance the 
troubleshooting experience.

It provides:

- incidents detection: heuristics to group individual alerts together to allow
better reasoning about the root cause of the issues.
- components mapping and ranking: an opinionated way to assign the alerts to high-level
components and rank them based on the importance of the components from the overall cluster
health perspective.

## Install

Login to a cluster using `oc login` command:

```
oc apply -f manifests/backend
```

## Usage

The Cluster Health Analyzer is a backend that exposes the results via Prometheus
metrics:

```
# Mapping of source signal to components and incident groups.
cluster_health_components_map
{
  # Identifier of the source signal type
  type="alert"

  # Matchers against the source labels
  src_alertname="KubeNodeNotReady",
  src_namespace="openshift-monitoring",
  src_severity="warning",

  # Identifier of the mapped component.
  layer="compute",
  component="compute",

  # Incident group id
  group_id="b8d9df3f-8245-4f5a-825d-15578a6c8397",

# Value represents the impact on the component severity
} -> 1
```

```
# Metadata about the components in the system
cluster_health_components
{
  # Identifier of the component
  component="compute", layer="compute"

# The value represents the ranking of the component: the lower number the higher
# importance of the component.
} -> 1
```

## Development and testing

If you want to contribute to the project head over to [development.md](development.md)
