#!/usr/bin/env bash
# Run mcpchecker evals end-to-end against a local setup.
#
# Unless already reachable, this script starts and later stops:
#   - Prometheus port-forward on localhost:9090 (thanos-querier, requires oc login)
#   - Alertmanager port-forward on localhost:9093 (requires oc login)
#   - The MCP server on localhost:8085 (make run-mcp equivalent)
#
# Environment variables:
#   TASK          run a single task by name (regex match)
#   CATEGORY      run all tasks with a matching category label
#   RUNS          number of runs per task (default: 1)
#   EVAL_CONFIG   eval config file name (default: eval.yaml)
#
# Creates synthetic firing alerts (evals/mcpchecker/alerts-prometheusrule.yaml)
# so the evals have incidents to query, and removes them when finished.

set -euo pipefail

# Give background commands separate process groups for cleanup.
set -m

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
EVAL_DIR="$ROOT_DIR/evals/mcpchecker"
MCPCHECKER_BIN="$ROOT_DIR/bin/mcpchecker"

PROM_URL="http://localhost:9090"
ALERTMANAGER_URL="http://localhost:9093"
MCP_URL="http://localhost:8085/healthz"
RULE_NAME="mcpchecker-eval-alerts"

PIDS=()
CREATED_RULE=0

cleanup() {
	if [ "$CREATED_RULE" = 1 ]; then
		echo "==> Removing synthetic eval alerts"
		oc delete prometheusrule -n openshift-monitoring -l test-type=mcpchecker-eval >/dev/null 2>&1 || true
	fi
	if [ "${#PIDS[@]}" -gt 0 ]; then
		echo "==> Stopping background processes"
		for pid in "${PIDS[@]}"; do
			kill -- "-$pid" 2>/dev/null || true
		done
		for pid in "${PIDS[@]}"; do
			wait "$pid" 2>/dev/null || true
		done
	fi
}
trap cleanup EXIT

# ensure_url <url> <name> <command...>
# Starts the command unless the url already answers, then waits for readiness.
ensure_url() {
	local url="$1" name="$2"
	shift 2

	if curl -sf --max-time 2 -o /dev/null "$url"; then
		echo "==> $name already reachable at $url, reusing it"
		return 0
	fi

	echo "==> Starting $name"
	"$@" &
	local pid=$!
	PIDS+=("$pid")

	echo "==> Waiting for $name at $url"
	local i
	for i in $(seq 1 60); do
		if curl -sf --max-time 2 -o /dev/null "$url"; then
			echo "==> $name is ready"
			return 0
		fi
		if ! kill -0 "$pid" 2>/dev/null; then
			echo "Error: $name exited unexpectedly (for port-forwards, check 'oc login')" >&2
			return 1
		fi
		sleep 1
	done

	echo "Error: $name not ready at $url within 60s" >&2
	return 1
}

if [ -z "${OPENAI_API_KEY:-}" ]; then
	echo "Error: OPENAI_API_KEY is not set. Export it before running the evals:" >&2
	echo '  export OPENAI_API_KEY="sk-..."' >&2
	exit 1
fi

if [ ! -x "$MCPCHECKER_BIN" ]; then
	echo "Error: $MCPCHECKER_BIN not found or not executable." >&2
	echo "  Run 'make install-mcpchecker' first." >&2
	exit 1
fi

command -v python3 >/dev/null 2>&1 || {
	echo "Error: python3 is required to verify tool arguments." >&2
	exit 1
}

cd "$ROOT_DIR"

# ensure_port_forward <url> <name> <command...>
# Like ensure_url, but only requires oc when the port-forward needs starting.
ensure_port_forward() {
	if ! curl -sf --max-time 2 -o /dev/null "$1"; then
		command -v oc >/dev/null 2>&1 || {
			echo "Error: oc is required for the Prometheus/Alertmanager port-forwards." >&2
			exit 1
		}
	fi
	ensure_url "$@"
}

ensure_port_forward "$PROM_URL/-/ready" "Prometheus" ./hack/listen-thanos.sh
ensure_port_forward "$ALERTMANAGER_URL/-/ready" "Alertmanager" ./hack/listen-alertmanager.sh
ensure_url "$MCP_URL" "MCP server" \
	go run -tags testonly ./main.go mcp --disable-auth-for-testing

# query_value <promql> — prints the scalar value of an instant query, or 0.
query_value() {
	curl -sG --max-time 5 "$PROM_URL/api/v1/query" --data-urlencode "query=$1" |
		python3 -c 'import json,sys
try:
    d = json.load(sys.stdin)
    print(d["data"]["result"][0]["value"][1])
except (KeyError, IndexError):
    print("0")'
}

command -v oc >/dev/null 2>&1 || {
	echo "Error: oc is required to create the synthetic eval alerts." >&2
	exit 1
}

if oc get prometheusrule "$RULE_NAME" -n openshift-monitoring >/dev/null 2>&1; then
	echo "==> Synthetic eval alerts already present, reusing them"
else
	echo "==> Creating synthetic eval alerts"
	oc apply -f "$EVAL_DIR/alerts-prometheusrule.yaml"
	CREATED_RULE=1
fi

echo "==> Waiting for synthetic alerts to fire and CHA to map them to components"
i=1
while :; do
	firing=$(query_value 'count(ALERTS{test_type="mcpchecker-eval",alertstate="firing"})')
	mapped=$(query_value 'count(cluster_health_components_map{src_alertname=~"etcdHighFsyncDurations|KubePodCrashLooping|etcdMembersDown"})')
	if [ "${firing:-0}" -ge 3 ] 2>/dev/null && [ "${mapped:-0}" -ge 3 ] 2>/dev/null; then
		echo "==> $firing synthetic alerts firing, $mapped mapped to components"
		break
	fi
	if [ "$i" -ge 60 ]; then
		echo "Error: synthetic alerts not ready after 5 minutes (firing=$firing mapped=$mapped)" >&2
		exit 1
	fi
	i=$((i + 1))
	sleep 5
done

cd "$EVAL_DIR"

RUNS="${RUNS:-1}"
EVAL_CONFIG="${EVAL_CONFIG:-eval.yaml}"

ARGS=(check "$EVAL_CONFIG" --runs "$RUNS")
if [ -n "${TASK:-}" ]; then
	ARGS+=(--run "$TASK" --verbose)
elif [ -n "${CATEGORY:-}" ]; then
	ARGS+=(--label-selector "category=$CATEGORY" --parallel 4)
else
	ARGS+=(--parallel 4)
fi

RESULT_FILE="$(
	"$MCPCHECKER_BIN" "${ARGS[@]}" |
		tee /dev/stderr |
		sed -n 's/^📄 Results saved to: //p'
)"
if [ -z "$RESULT_FILE" ]; then
	echo "Error: mcpchecker did not report a results file." >&2
	exit 1
fi

"$MCPCHECKER_BIN" result summary "$RESULT_FILE"
echo "==> Verifying tool arguments"
python3 "$EVAL_DIR/verify-tool-arguments.py" "$RESULT_FILE"
