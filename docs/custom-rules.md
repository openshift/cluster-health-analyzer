# Custom incident rules: branch history and configuration

This document covers two branches built on top of `origin/main` (release-1.1 line):

| Branch | Fork point | Status | Purpose |
|---|---|---|---|
| `custom-rules` (tip: `ca4e5ec`, "draft crd spec") | `774eadb` | draft, superseded | Made incident grouping rules configurable via a custom CRD |
| `custom-rules-configmap` (tip: `40a61e9`) | `custom-rules` | current | Same feature, delivered via a plain ConfigMap instead of the CRD |

## Background: what was changed

On `main`, the heuristics that group related alerts into incidents (which labels
to match on, how far apart in time alerts may be) were hardcoded in
`pkg/processor`. Both branches externalize them into cluster configuration that
is validated and hot-reloaded at runtime, without restarting the analyzer:

- `pkg/processor/rules.go` — rule schema, validation and parsing
- `pkg/processor/rules_watcher.go` — watches the configuration source, publishes immutable `RulesSnapshot`s
- `pkg/processor/incidents.go` — grouping engine now driven by the parsed rules
- Tests: `rules_test.go`, `rules_schema_test.go`, `rules_watcher_test.go` and updates across `server`, `processor`, `health`, `mcp`

## Branch `custom-rules` (draft, superseded)

Introduced the `IncidentRule` CRD (`monitoring.openshift.io/v1alpha1`):

- `manifests/backend/08_incident_rule_crd.yaml` — CRD with OpenAPI schema and CEL validations
- `manifests/backend/09_incident_rule_defaults.yaml` — `IncidentRule/default` replicating the legacy hardcoded matchers
- `manifests/backend/10_incident_rule_rbac.yaml` — ClusterRole for `get/list/watch` on `incidentrules`
- Watcher used a dynamic informer over the CRD's GVR

Downside: shipping a custom CRD requires installing it cluster-wide and paying
the alpha-CRD lifecycle (defaulting, conversion, validation moving server-side).

## Branch `custom-rules-configmap` (current)

Replaces the CRD with a standard ConfigMap, `cluster-health-analyzer-config`,
watched in the pod's own namespace (read from the service account namespace
file, fallback `default`). No cluster extension is required.

### Configuration format

The rules live in the `rules.yaml` data key; each entry is a named rule with a
priority and a list of strategies:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: cluster-health-analyzer-config
  namespace: openshift-cluster-health-analyzer
data:
  rules.yaml: |
    rules:
      - name: default
        priority: 0
        strategies:
          - match:
              exact: true
            within: 24h
            from: end
          ...
```

Strategy fields:

| Field | Meaning |
|---|---|
| `match` | One operator only: `exact: true`, `allOf: [labels]`, `anyOf: [labels]`, or `{}` (catch-all) |
| `within` | Time window (positive duration, units `h`/`m`/`s`, max 32 chars) |
| `from` | Window anchor: `end` (end of last matched alert) or `start` (start of most recent matched alert) |
| `excludeAlerts` | Optional list of label matchers; matching alerts are never grouped by the strategy |

Rules are sorted by `priority` then `name`; lower priority number wins. An
invalid config is logged and produces an empty snapshot (the analyzer refuses
to start on an empty initial load: `no valid IncidentRules loaded`).

### Manifests

- `manifests/backend/08_incident_rule_crd.yaml` — removed
- `manifests/backend/09_incident_rule_defaults.yaml` — now the ConfigMap with the default rules
- `manifests/backend/10_incident_rule_rbac.yaml` — RBAC narrowed to `get/list/watch` on `configmaps` with `resourceNames: [cluster-health-analyzer-config]`

### Trade-offs vs the CRD

- No server-side schema validation: a malformed config is only rejected by the
  analyzer's own parser (logged, previous snapshot kept until a valid one arrives)
- No CRD install step; works on any cluster with plain RBAC
- `LoadRulesFile` (used by `simulate` and tests) accepts either the ConfigMap
  manifest or the bare `rules.yaml` payload

### Verifying

```sh
make test          # unit tests (all pass on this branch)
go test ./pkg/processor/ ./pkg/server/
```
