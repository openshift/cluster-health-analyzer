#!/bin/bash

oc port-forward -n openshift-monitoring $(oc get pods -n openshift-monitoring -l app.kubernetes.io/instance=thanos-querier -o jsonpath="{.items[0].metadata.name}") 9090:9090
