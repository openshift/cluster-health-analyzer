# Debug endpoints

Some endpoints exist only to inspect the internal state of a running
Cluster Health Analyzer (CHA). They are **not part of the supported API**:
their shape can change at any time, and they are compiled out of production
binaries.

## `/debug/incident-state`

Returns the processor's complete in-memory matcher state as JSON. It is the
tool for understanding the **matcher lifecycle**: which incidents exist,
which matchers (label selectors and temporal windows) they carry, how their
time references move, what gets pruned, and what a live rules reload
regenerates.

### Availability

The endpoint is included only in builds compiled with the `testonly` tag:

```sh
make run        # go run -tags testonly — includes the endpoint
make build-dev  # go build -tags testonly — includes the endpoint
make build      # production build — endpoint absent (no-op registration)
```

When served through the API server, the endpoint inherits the same TLS and
authentication as `/metrics`. With `make run`, authentication is disabled
for testing.

To check which flavor a binary is, run `cluster-health-analyzer version`:
it prints the version and `dev build` (testonly tag, endpoint included) or
`production build` (endpoint absent). This also works on a binary extracted
from a container image.

### Response

`GET /debug/incident-state` returns one object with three arrays:

| Field | Meaning |
|---|---|
| `groups` | Every matcher currently held in memory, one entry per matcher (an incident typically has several). |
| `assignments` | Recovered or reload-preserved alert-to-incident assignments, with the exact labels that anchor them. |
| `forced_roots` | Shared root matchers created for recovered incident IDs. |

Each entry in `groups`:

| Field | Meaning |
|---|---|
| `group_id` | Matcher's own ID. Empty for non-root matchers: only `root_group_id` is set on doors attached to an incident. |
| `root_group_id` | The incident ID the matcher belongs to (`group_id` in `cluster_health_components_map`). |
| `strategy` | The rule and strategy that produced the matcher: `rule_name`, `priority`, `operator` (`exact`, `allOf`, `anyOf`, or empty for time-only), `keys`, `within`, `retention`, `from`, `expand`. |
| `start`, `modified`, `end` | The matcher's timeline, as maintained by processing. |
| `reference_time` | The time `within` is measured from: `end` for `from: end` strategies, `modified` for `from: start`. Zero for matchers without a strategy (for example Watchdog roots). |
| `selectors` | Label selectors of the matcher, as label/value maps. Absent for time-only matchers, which accept any alert within their window. |

Each entry in `assignments`:

| Field | Meaning |
|---|---|
| `labels` | Complete alert labels of the assignment. An alert with exactly these labels keeps its incident. |
| `start`, `end` | Time range the assignment covers. |
| `incident_id` | The incident the alert is anchored to. |

### How to read the lifecycle

The endpoint shows the **state**; transitions are inferred by polling and
diffing. For example:

```sh
make proxy
make run
watch -n 5 'curl -sk https://localhost:8443/debug/incident-state | jq'
```

What to look for:

- **Incident creation** — a new `root_group_id` appears with its initial
  matchers. Time-only matchers show no `selectors`.
- **Window movement** — each processed observation updates `modified`
  (`from: start`) or `end` (`from: end`), moving `reference_time` forward.
  A matcher stops moving once no alert selects it, even if the incident is
  still active through other matchers.
- **Pruning** — a group disappears once its retention
  (`max(24h, within)`; `max(120h, within)` for exact) has elapsed from its
  reference time.
- **Rules reload** — after a valid ConfigMap change, the same
  `root_group_id`s reappear with regenerated matchers and time references
  reset to the reload time. This is the state behind
  [temporal absorption after a reload](incident-rules-config-map.md#temporal-absorption-after-a-reload):
  an incident that is still firing keeps a time-only matcher that can
  capture re-firing or unrelated alerts within `within`, despite the new
  rules.
- **Restart** — `assignments` shows which historical alert-to-incident
  assignments were recovered from `ALERTS` and
  `cluster_health_components_map` history.

### Implementation notes

- The snapshot is a deep copy taken under a read lock; it is safe to poll
  while processing runs, and it never mutates processor state.
- The payload is debug output, not a contract: fields are added or renamed
  as needed.
- Source: `pkg/processor/state_snapshot.go` (snapshot types),
  `pkg/server/incident_state_testonly.go` (handler, `testonly` tag),
  `pkg/server/incident_state.go` (no-op stub for production builds).
