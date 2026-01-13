#!/bin/bash
#
# Deploys cluster-health-analyzer for integration testing.
# This script patches manifests with the specified image and deploys to the cluster.
#
# Required: oc login to target cluster before running.
#
# Environment variables:
#   CHA_IMAGE        - Container image to deploy (default: quay.io/openshiftanalytics/cluster-health-analyzer:latest)
#   MANIFESTS_PATH   - Path to manifest files (default: manifests/backend)
#   DEPLOYMENT_NAME  - Name of the deployment (default: cluster-health-analyzer)
#   NAMESPACE        - Target namespace (default: openshift-cluster-health-analyzer)
#

set -euo pipefail

# Verify prerequisites
if ! command -v oc &>/dev/null; then
    echo "Error: 'oc' command not found. Please install the OpenShift CLI."
    exit 1
fi

if ! oc whoami &>/dev/null; then
    echo "Error: Not logged into OpenShift cluster. Run 'oc login' first."
    exit 1
fi

if ! command -v yq &>/dev/null; then
    echo "Error: 'yq' command not found. Install from https://github.com/mikefarah/yq"
    exit 1
fi

# Configuration with defaults
CHA_IMAGE="${CHA_IMAGE:-quay.io/openshiftanalytics/cluster-health-analyzer:latest}"
MANIFESTS_PATH="${MANIFESTS_PATH:-manifests/backend}"
DEPLOYMENT_NAME="${DEPLOYMENT_NAME:-cluster-health-analyzer}"
NAMESPACE="${NAMESPACE:-openshift-cluster-health-analyzer}"

echo "=== Cluster Health Analyzer Deployment ==="
echo "CHA_IMAGE: ${CHA_IMAGE}"
echo "MANIFESTS_PATH: ${MANIFESTS_PATH}"
echo "DEPLOYMENT_NAME: ${DEPLOYMENT_NAME}"
echo "NAMESPACE: ${NAMESPACE}"

# Create temp directory for patched manifests
TEMP_MANIFESTS=$(mktemp -d)
trap "rm -rf ${TEMP_MANIFESTS}" EXIT

echo "=== Patching manifests with image ==="
cp "${MANIFESTS_PATH}"/*.yaml "${TEMP_MANIFESTS}/"
yq eval -i ".spec.template.spec.containers[0].image = \"${CHA_IMAGE}\"" \
    "${TEMP_MANIFESTS}/05_deployment.yaml"

echo "=== Deploying cluster-health-analyzer ==="
oc apply -f "${TEMP_MANIFESTS}/"

echo "=== Waiting for deployment to be ready ==="
oc wait --for=condition=available --timeout=300s \
    "deployment/${DEPLOYMENT_NAME}" -n "${NAMESPACE}"

echo "=== Checking pod status ==="
oc get pods -n "${NAMESPACE}" -l "app.kubernetes.io/name=${DEPLOYMENT_NAME}"

echo "=== Deployment successful ==="

