"""Verify get_incidents arguments and successful responses."""

import argparse
import json
import sys


EXPECTED_ARGUMENTS = {
    "backend-reachability": {},
    "list-incidents": {},
    "critical-incidents": {"min_severity": "critical"},
    "recent-incidents": {"time_range": 24},
    "cluster-health-summary": {},
}


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
