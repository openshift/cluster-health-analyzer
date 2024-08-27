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

Login to a cluster using `oc login` command. First we would need to create a manifest copying existing 
ConfigMap in openshift-monitoring namespace (required by TLS endpoint to forward metrics to in-cluster Prometheus)

```
oc get configmap metrics-client-ca --namespace=openshift-monitoring -o yaml \
	| yq e '.metadata.namespace |= sub("openshift-monitoring", "openshift-cluster-health-analyzer")' - \
	| yq e 'del(.metadata.resourceVersion)' - \
	| yq e 'del(.metadata.uid)' - \
	| yq e 'del(.metadata.labels)' - \
	| yq e 'del(.metadata.creationTimestamp)' - > manifests/backend/02_metrics_client_ca_configmap.yaml

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

## Testing

### Data simulation

For development purposes, it's useful to have some data filled in Prometheus.

It's possible to generate sample alerts + corresponding components and incidents
mappings via the following script:

``` sh
go run ./main.go simulate
```

This script generates `cluster-health-analyzer-openmetrics.txt` file. It can be
then turned into tsdb files via `promtool`, that's available as part of prometheus
installation:

``` sh
promtool tsdb create-blocks-from openmetrics cluster-health-analyzer-openmetrics.txt
```

Finally, one can copy the files to the cluster that's running the health analyzer:

``` sh
for d in data/*; do
  echo $d
  kubectl cp $d openshift-user-workload-monitoring/prometheus-user-workload-0:/prometheus -c prometheus
done
```

Once finished, the data should appear in the target cluster.
