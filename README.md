# Cluster Health Analyzer

An analyzer for OpenShift cluster health data.

## Overview

The Cluster Health Analyzer process incoming stream of health signal from
the OpenShift cluster and enriches it to provide better views at the data
to enhance the troubleshooting experience.

It provides:

- incidents detection: heuristics to group individual alerts together to allow
better reasoning about the root cause of the issues.
- components mapping and ranking: opinionated way to assign the alerts to high-level
components and ranking based on the importance of the components from overall cluster
health perspective.

## Install

```
oc apply -f manifests/backend -f manifests/frontend
```

## Usage

Cluster health analyzer is a backend that exposes the results via Prometheus
metrics:

```
# Mapping of source signal to components and incident groups.
cluster:health:components:map
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
cluster:health:components
{
  # Identifier of the component
  component="compute", layer="compute"

# The value represents the ranking of the component: the lower number the higher
# importance of the component.
} -> 1
```

See https://github.com/openshift/cluster-health-console-prototype of an example
usage of the data for incidents navigation.
