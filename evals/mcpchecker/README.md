# MCPChecker Evals

Evaluations for cluster-health-analyzer using
[mcpchecker](https://github.com/mcpchecker/mcpchecker) — tests that AI agents
can discover and correctly use the `get_incidents` MCP tool against a live
Prometheus/Alertmanager backend.

## Pre-requisites

- [mcpchecker](https://github.com/mcpchecker/mcpchecker#install) installed
  (v0.0.22+) — run `make install-mcpchecker` from the repo root
- OpenShift cluster access (`oc login`)
- Python 3 for the argument/response checks
- `OPENAI_API_KEY` exported (see below)

`make run-evals` starts everything else automatically and stops it
when finished: the Prometheus (thanos-querier) and Alertmanager
port-forwards, and the MCP server (equivalent to `make run-mcp` on
http://localhost:8085). Anything already listening on localhost:9090,
localhost:9093 or localhost:8085 is reused as-is.

## Environment Variables

mcpchecker uses two separate LLM roles:

- **Agent** — the LLM that interacts with the MCP server. This is the model
  being evaluated.
- **Judge** — a separate LLM that evaluates the agent's output against the
  expected criteria defined in each task.

Both are configured as `builtin.llm-agent` with `openai:gpt-5-nano` and share
the same API key:

```bash
export OPENAI_API_KEY="sk-..."
```

For other providers, see the
[mcpchecker agent docs](https://github.com/mcpchecker/mcpchecker/blob/main/docs/how-to/configure-agents.md)
and update the `agent` and `llmJudge.ref` sections in `eval.yaml`.

## Running Evals

```bash
make run-evals                       # run all tasks in parallel
make run-evals TASK=critical-incidents  # single task, verbose
make run-evals TASK="incidents"      # regex match
make run-evals CATEGORY=incidents    # all tasks with a label
```

Additional mcpchecker tools:

```bash
cd evals/mcpchecker
../../bin/mcpchecker result summary mcpchecker-cluster-health-tools-out.json
```

## Task Structure

Tasks live under `tasks/` and are organized by category. Each task YAML
defines the prompt and LLM judge criteria; expected tool usage, call bounds
are asserted in `eval.yaml`.

| Directory             | Description                                   |
|-----------------------|-----------------------------------------------|
| `tasks/incidents/`    | get_incidents smoke tests, filters, reasoning |

All tasks include `labels` for filtering with `--label-selector`
(e.g. `category=incidents`).

## Verification

mcpchecker checks tool usage and call counts, and uses one LLM judge to
assess the final answer against each task's criteria.

The runner also invokes `verify-tool-arguments.py` to require a successful
`get_incidents` response for each task and check the requested severity/time
filters. This is a deterministic check, not another LLM judge. Failures
exit nonzero and are reported separately from mcpchecker's summary.

The judge does not receive the recorded tool responses, so passing does
not prove factual grounding. When reviewing results, compare `taskOutput`
with `callHistory` in the results JSON.

## Adding New Tasks

1. Create a new YAML file under the appropriate `tasks/` subdirectory.
2. Add a corresponding `taskSet` entry in `eval.yaml` with the expected
   tool assertions.
3. Add the task to `EXPECTED_ARGUMENTS` in `verify-tool-arguments.py`,
   using an empty mapping when no specific argument values are required.

## CI

Not wired up yet. The setup is intentionally local-first; a CI job can reuse
`make install-mcpchecker` and `make run-evals` once a stable
backend dataset and API key handling are decided.
