# Development

This document describes the process for running cluster-health-analyzer locally for development purposes.

## Running locally

In order to run the code outside of Kubernetes environment:

1. start port-forwarding of thanos querier from existing OpenShift installation:

``` sh
make proxy
```
or
``` sh
./hack/listen-thanos.sh
```

2. run the service with disabled auth

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

When logged into an OpenShift cluster with `$KUBECONFIG` variable pointing
to the appropriated kubectl configuration, one can run the authenticated version
of the service with:

``` sh
go run ./main.go serve --kubeconfig $KUBECONFIG
```

Note that because it will require proper authentication and your local machine 
does not have client CAs you would no longer be able to retrieve the metrics locally.

## Testing

Before sending your changes make sure to run `make precommit` (this will run both `make lint` and `make test`)
to avoid CI failures for your PR. There are other useful commands such as `make proxy` and `make deploy`, For
full list run `make help`.

## Data simulation

For development purposes, it's useful to have some data filled in Prometheus.

It's possible to generate sample alerts + corresponding components and incidents
mappings via the following script:

``` sh
go run ./main.go simulate
```
or
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
