# Development

This document describes the process of running the Cluster Health Analyzer locally for development purposes.

## Running locally

To run the code outside of the Kubernetes environment:

1. Start the port forwarding of the thanos querier from the existing OpenShift installation:

``` sh
make proxy
```
or
``` sh
./hack/listen-thanos.sh
```

2. Run the service with disabled auth:

``` sh
make run
```
or
``` sh
go run ./main.go serve --disable-auth-for-testing
```

The metrics should be exposed over https with self-signed certificates:

``` sh
curl -k https://localhost:8443/metrics
```

If you are logged into an OpenShift cluster with the `$KUBECONFIG` variable pointing
to the appropriate kubectl configuration, you can run the authenticated version
of the service with:

``` sh
go run ./main.go serve --kubeconfig $KUBECONFIG
```

Note that since it requires proper authentication and your local machine 
does not have client CAs, you will no longer be able to retrieve the metrics locally.

## Testing

Before sending your changes, make sure to run `make precommit` (this will run both `make lint` and `make test`)
to avoid CI failures for your PR. There are other useful commands such as `make proxy` and `make deploy`. For
full list run `make help`.

## Data simulation

For development purposes, it's useful to have some data populated in Prometheus.

It's possible to generate sample alerts + corresponding component and incident
mappings using the following command:

``` sh
SCENARIO=input.csv make simulate
```
or
``` sh
go run ./main.go simulate --scenario input.csv
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

``` sh
promtool tsdb create-blocks-from openmetrics cluster-health-analyzer-openmetrics.txt
```

Finally, you copy the files to the cluster that's running the Health Analyzer:

``` sh
for d in data/*; do
  echo $d
  kubectl cp $d openshift-monitoring/prometheus-k8s-0:/prometheus -c prometheus
done
```

Once complete, the data will appear in the target cluster.
