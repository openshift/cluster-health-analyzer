#!/usr/bin/env bash
# Build and deploy the current checkout, then own the proxies for the test run.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

# Prow's NAMESPACE identifies the CI build namespace, not the target deployment.
if [[ -n "${PROW_JOB_ID:-}" ]]; then
    NAMESPACE="${CHA_NAMESPACE:-openshift-cluster-health-analyzer}"
else
    NAMESPACE="${CHA_NAMESPACE:-${NAMESPACE:-openshift-cluster-health-analyzer}}"
fi
export NAMESPACE
export DEPLOYMENT_NAME="${CHA_DEPLOYMENT_NAME:-${DEPLOYMENT_NAME:-cluster-health-analyzer}}"
export MANIFESTS_PATH="${CHA_MANIFESTS_PATH:-${MANIFESTS_PATH:-manifests/backend}}"

for cmd in oc yq go; do
    command -v "${cmd}" >/dev/null || { echo "Error: ${cmd} is required" >&2; exit 1; }
done
oc whoami >/dev/null
if [[ "$(yq '.metadata.name' "${MANIFESTS_PATH}/01_namespace.yaml")" != "${NAMESPACE}" ||
      "$(yq '.metadata.namespace' "${MANIFESTS_PATH}/05_deployment.yaml")" != "${NAMESPACE}" ||
      "$(yq '.metadata.name' "${MANIFESTS_PATH}/05_deployment.yaml")" != "${DEPLOYMENT_NAME}" ]]; then
    echo "Error: NAMESPACE and DEPLOYMENT_NAME must match MANIFESTS_PATH." >&2
    exit 1
fi

workdir=$(mktemp -d)
pids=()
test_pid=
# Invoked by the EXIT trap, including signal-triggered exits.
# shellcheck disable=SC2329
cleanup() {
    local status=$?
    trap - EXIT
    # Keep proxies alive while Ginkgo removes its rules and checks reconciliation.
    if [[ -n "${test_pid}" ]]; then
        kill -TERM "${test_pid}" 2>/dev/null || true
        wait "${test_pid}" 2>/dev/null || true
    fi
    for pid in "${pids[@]}"; do
        kill "${pid}" 2>/dev/null || true
    done
    for pid in "${pids[@]}"; do
        wait "${pid}" 2>/dev/null || true
    done
    if (( status != 0 )); then
        for log in "${workdir}"/*.log; do
            [[ ! -f "${log}" ]] || cat "${log}" >&2
        done
    fi
    rm -rf "${workdir}"
    exit "${status}"
}
trap 'cleanup' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# Run the compiled suite directly so signals reach it, not a go-run wrapper.
go test -c -o "${workdir}/integration.test" ./test/integration/tests

if [[ -z "${CHA_IMAGE:-}" ]]; then
    oc apply -f "${MANIFESTS_PATH}/01_namespace.yaml"
    build_name="cha-integration-$(date +%s)-${RANDOM}"
    # Upload only build inputs, never kubeconfigs or unrelated working-tree files.
    mkdir "${workdir}/source"
    cp -r Dockerfile go.mod go.sum main.go cmd pkg "${workdir}/source/"
    oc new-build --binary --strategy=docker --name="${build_name}" \
        --to="${build_name}:latest" -n "${NAMESPACE}"
    oc start-build "${build_name}" --from-dir="${workdir}/source" \
        --follow --wait -n "${NAMESPACE}"
    CHA_IMAGE=$(oc get istag "${build_name}:latest" -n "${NAMESPACE}" \
        -o jsonpath='{.image.dockerImageReference}')
    if [[ -z "${CHA_IMAGE}" ]]; then
        echo "Error: build produced no image reference" >&2
        exit 1
    fi
fi
export CHA_IMAGE
bash ./hack/deploy-integration.sh

start_proxy() {
    local name=$1 selector=$2 remote_port=$3 pod pid port
    pod=$(oc get pods -n openshift-monitoring -l "${selector}" \
        -o jsonpath='{.items[0].metadata.name}')
    [[ -n "${pod}" ]] || { echo "Error: no ${name} pod found" >&2; return 1; }
    # Let oc allocate a free local port; never accidentally use another local server.
    oc port-forward -n openshift-monitoring --address=127.0.0.1 \
        "pod/${pod}" ":${remote_port}" >"${workdir}/${name}.log" 2>&1 &
    pid=$!
    pids+=("${pid}")
    for ((i = 0; i < 60; i++)); do
        kill -0 "${pid}" 2>/dev/null || return 1
        port=$(sed -n 's/^Forwarding from 127\.0\.0\.1:\([0-9]*\) -> .*/\1/p' "${workdir}/${name}.log" | head -1)
        if [[ -n "${port}" ]]; then
            printf -v "${name}_URL" 'http://127.0.0.1:%s' "${port}"
            export "${name}_URL"
            return 0
        fi
        sleep 1
    done
    echo "Error: ${name} port-forward timed out" >&2
    return 1
}
start_proxy THANOS app.kubernetes.io/instance=thanos-querier 9090
start_proxy ALERTMANAGER app.kubernetes.io/name=alertmanager 9093
# The pod's HTTP endpoint does not need a route bearer token.
unset THANOS_TOKEN

no_color=false
[[ -z "${CI:-}" ]] || no_color=true
"${workdir}/integration.test" -test.v -test.timeout=2h10m \
    -ginkgo.v -ginkgo.no-color="${no_color}" \
    -ginkgo.label-filter=long -ginkgo.timeout=2h &
test_pid=$!
while kill -0 "${test_pid}" 2>/dev/null; do
    for pid in "${pids[@]}"; do
        if ! kill -0 "${pid}" 2>/dev/null; then
            echo "Error: a monitoring proxy exited during the tests" >&2
            exit 1
        fi
    done
    sleep 1
done
status=0
wait "${test_pid}" || status=$?
test_pid=
exit "${status}"
