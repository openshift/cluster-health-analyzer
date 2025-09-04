#!/bin/bash

oc port-forward -n openshift-monitoring $(oc get pods -n openshift-monitoring -l app.kubernetes.io/name=alertmanager -o jsonpath="{.items[0].metadata.name}") 9093:9093
