"""Verify get_incidents arguments and successful responses."""

import argparse
import json
import re
import sys
from pathlib import Path


EXPECTED_ARGUMENTS = {
    "backend-reachability": {},
    "list-incidents": {},
    "critical-incidents": {"min_severity": "critical"},
    "recent-incidents": {"time_range": 24},
    "cluster-health-summary": {},
}

# Synthetic alerts defined in alerts-prometheusrule.yaml and applied by
# hack/run-mcpchecker-eval.sh; labelled test_type=mcpchecker-eval.
def synthetic_alerts_from_rule():
    """Alert name -> severity from alerts-prometheusrule.yaml, the single
    source of truth for the synthetic eval alerts."""
    rule = Path(__file__).with_name("alerts-prometheusrule.yaml").read_text()
    alerts, name = {}, None
    for line in rule.splitlines():
        match = re.match(r"\s*- alert:\s*(\S+)", line)
        if match:
            name = match.group(1)
            continue
        match = re.match(r"\s*severity:\s*(\w+)", line)
        if match and name and name not in alerts:
            alerts[name] = match.group(1)
    return alerts


SYNTHETIC_ALERTS = synthetic_alerts_from_rule()


def returned_synthetic_alerts(calls):
    """Synthetic alert names returned by successful get_incidents calls."""
    names = set()
    for call in calls:
        if not call.get("success") or call.get("result") is None:
            continue
        if call["result"].get("isError"):
            continue
        for content in call["result"].get("content") or []:
            match = re.search(
                r"<DATA>\n(.*)\n</DATA>", content.get("text") or "", re.S
            )
            if not match:
                continue
            payload = json.loads(match.group(1))
            for incident in payload["incidents"]["items"]:
                for alert in incident["alerts"]:
                    if alert.get("test_type") == "mcpchecker-eval":
                        names.add(alert["name"])
    return names


def verify_results(results):
    errors = []
    if not results:
        errors.append("no task results found (check TASK/CATEGORY selection)")
        return errors
    for result in results:
        name = result["taskName"]
        label = f"{name} (run {result.get('runIndex', 0) + 1})"
        if not result.get("taskPassed") or not result.get("allAssertionsPassed"):
            errors.append(
                f"{label}: mcpchecker verification or assertions failed"
            )

        if name not in EXPECTED_ARGUMENTS:
            continue

        history = result.get("callHistory") or {}
        calls = [
            call
            for call in history.get("ToolCalls", []) or []
            if call["serverName"] == "cluster-health"
            and call["name"] == "get_incidents"
        ]
        if not calls:
            errors.append(f"{label}: no get_incidents calls recorded")
            continue

        for index, call in enumerate(calls, 1):
            arguments = call["request"]["Params"].get("arguments") or {}
            for key, expected in EXPECTED_ARGUMENTS[name].items():
                actual = arguments.get(key)
                if key == "min_severity" and isinstance(actual, str):
                    actual = actual.lower()
                if actual != expected:
                    errors.append(
                        f"{label}, call {index}: expected {key}={expected!r}, "
                        f"got {arguments.get(key)!r}"
                    )

        if not any(
            call.get("success")
            and call.get("result") is not None
            and not call["result"].get("isError")
            for call in calls
        ):
            errors.append(f"{label}: no successful get_incidents response")

        # Tasks filtering on min_severity can only expect alerts at or above
        # that severity; all other tasks must see every synthetic alert.
        min_severity = EXPECTED_ARGUMENTS.get(name, {}).get("min_severity")
        expected = {
            alert_name
            for alert_name, severity in SYNTHETIC_ALERTS.items()
            if min_severity is None or severity == min_severity
        }
        missing = expected - returned_synthetic_alerts(calls)
        if missing:
            errors.append(
                f"{label}: synthetic alerts {sorted(missing)} "
                "missing from get_incidents responses"
            )

    return errors


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("results_file")
    args = parser.parse_args()
    with open(args.results_file, encoding="utf-8") as stream:
        errors = verify_results(json.load(stream)["results"])
    for error in errors:
        print(error, file=sys.stderr)
    sys.exit(bool(errors))
