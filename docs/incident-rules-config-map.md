# Incident rules via ConfigMap

Cluster Health Analyzer (CHA) groups related firing Prometheus alerts into
**incidents**. Each incident has a `group_id`, exposed in the
`cluster_health_components_map` metric. Incident rules let you configure which
alerts belong together using labels, time windows, and precedence, without
changing the CHA binary.

CHA reads rules from a Kubernetes ConfigMap and watches it for updates. This guide
explains configuration, grouping behavior, verification, and known limits.
Component health evaluation, TLS, authentication, and silence evaluation are
configured separately.

## Contents

- [Compatibility expectations](#compatibility-expectations)
  - [Introduced behavioural changes](#introduced-behavioural-changes)
  - [Known limitations](#known-limitations)
- [Configuration format and deployment](#configuration-format-and-deployment)
- [Processing responsibilities](#processing-responsibilities)
- [Namespace selection](#namespace-selection)
- [Validation and configuration lifecycle](#validation-and-configuration-lifecycle)
- [Operators and candidate selection](#operators-and-candidate-selection)
- [Rules prevail over batching](#rules-prevail-over-batching)
- [Non-retroactive rule changes](#non-retroactive-rule-changes)
- [Restart and identity recovery](#restart-and-identity-recovery)
- [Seven-day horizon and pruning](#seven-day-horizon-and-pruning)
- [Verification and compatibility](#verification-and-compatibility)
- [Implementation map and open details](#implementation-map-and-open-details)

## Compatibility expectations

> **Review the introduced behavioural changes and known limitations before
> deploying the feature.** The default rules preserve the main grouping strategies,
> not every historical grouping outcome or incident ID.

The defaults retain exact matching, matching by selected labels ("fuzzy" matching),
and time-only matching. Reusing an incident ID when an alert returns after resolving
is also existing behavior: the earlier hardcoded implementation already had an
exact fallback window of five days, now expressed as `within: 120h`.

### Introduced behavioural changes

| Area | Behavior introduced by the feature |
|---|---|
| [Initial configuration](#startup) | Incident processing requires a valid ConfigMap at startup; there is no silent built-in fallback. |
| [Custom rules](#configuration-format-and-deployment) | Configured rules replace the default strategy list rather than extending it. Default matching paths and fallback windows are not implicitly retained. |
| [Batch grouping](#rules-prevail-over-batching) | Alerts group only through rules, not merely because they arrived together. |
| [Live configuration updates](#runtime) | Valid changes are adopted without restarting CHA. Invalid updates or ConfigMap deletion retain the last valid snapshot; a restart still requires valid configuration. |
| [Namespace](#namespace-selection) | Explicit environment override takes precedence over pod discovery and `default`. |
| [Time windows](#validation-and-configuration-lifecycle) | Matching windows are configurable up to seven days; larger values are rejected. |
| [History and pruning](#seven-day-horizon-and-pruning) | Startup queries seven days of history instead of four. Pruning uses each strategy's time reference to avoid discarding still-eligible matchers. |

### Known limitations

- **Historical identity recovery — inherited limitation.** Ambiguous exported
  labels or missing history can prevent ID recovery. See
  [recovery evidence](#recovery-evidence-is-lossy) for the cause and behavior.
- **Live reload does not guarantee assignment preservation.** Only currently
  firing alerts seed the rebuild, and matcher time references reset. See
  [reload limitations](#reload-implementation-and-limits).
- **Fully tied candidates have no guaranteed global tie-breaker.** See
  [candidate ranking](#ranking-eligible-candidates).
- **Cluster-level verification remains necessary.** Unit tests do not establish
  real lifecycle behavior, long-running recovery, or operator packaging correctness.
  See [verification and compatibility](#verification-and-compatibility).

## Configuration format and deployment

### Source and schema

CHA reads one ConfigMap named `cluster-health-analyzer-config`, using its
`rules.yaml` data key. No custom resource definition (CRD) or additional
configuration service is required.

A **rule** is a named, prioritized list of **strategies**. Each strategy specifies
which labels to compare and how far apart observations may be. Strategies are
alternatives: an alert need not satisfy every strategy, but must pass the label,
exclusion, and time checks of the strategy used to match it.

This complete example groups alerts by node within a 24-hour window. Alerts on
different nodes do not group together through this configuration, even if they
share a namespace or arrive together. Alerts without a `node` label cannot match
this strategy; they are still processed, but may receive a new incident ID on
each observation because there is no fallback strategy.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: cluster-health-analyzer-config
  namespace: openshift-cluster-health-analyzer
data:
  rules.yaml: |
    rules:
      - name: by-node
        priority: 0
        strategies:
          - match:
              allOf: [node]
            within: 24h
            from: start
```

| Field | Meaning |
|---|---|
| `rules` | Required, nonempty list of rules. |
| `name` | Required, nonempty rule name; also used to break priority ties. Use distinct names for predictable ordering. |
| `priority` | Optional signed 32-bit integer; defaults to `0`. Lower values take precedence. |
| `strategies` | Required, nonempty ordered list. Earlier strategies take precedence within a rule. |
| `match` | Required. Use exactly one of `exact: true`, `allOf: [keys]`, `anyOf: [keys]`, or an empty map `{}` for time-only matching. |
| `within` | Required positive duration, at most `168h` (seven days). Use `h`, `m`, or `s`, including combinations such as `1h30m`; maximum string length is 32 characters. |
| `from` | Required: `start` or `end`. Selects the matcher's time reference, explained below. |
| `excludeAlerts` | Optional list of label/value selectors that exclude alerts from this strategy only. Not supported with `match: {}`. |

The label operators compare values taken from observed alerts; `allOf: [node]`
names a label key, not a fixed node value. See
[operator semantics](#operator-semantics) for examples.

The parser and file loader are in [rules.go](../pkg/processor/rules.go).
`LoadRulesFile`, used by simulation and tests, accepts either a complete ConfigMap
manifest or the bare `rules.yaml` payload. It is not a server fallback when the
ConfigMap is absent.

### Understanding `from: start` and `from: end`

When evaluating whether an alert can join an incident, a strategy asks: **is this
alert close enough in time to an observation already associated with that
incident?** `from` selects the reference time, and `within` sets how much later
the incoming alert may start.

First, distinguish the alert's firing history from its incident assignment:

| Historical metric | What it tells CHA |
|---|---|
| `ALERTS` | Which alert was observed firing, with what labels, and when. Consecutive samples form an observation interval. |
| `cluster_health_components_map` | Which incident an alert was assigned to, through its `group_id` and exported source labels. This is separate evidence used to recover assignments after a restart. |

**An alert observation interval is not an incident's lifetime or the period when
an incident metric was published.** The alert samples alone do not tell CHA
whether an incident was previously published.

CHA stores label comparisons and timing information in a **matcher** associated
with an incident. One incident can have several matchers, each with its own time
reference:

| Setting | Reference used by that matcher |
|---|---|
| `from: start` | The start of its most recently matched observation. This moves when the matcher is updated; it is not fixed to the incident's creation time. |
| `from: end` | The latest observed end recorded by the matcher. This is not the incident's resolution time. |

#### Example: replaying historical alerts after a restart

Compare two strategies that both use `allOf: [node]` and `within: 30m`, differing
only in `from`:

1. Historical `ALERTS` samples show **alert A on worker-1 firing from 10:00 to
   11:00**. CHA reconstructs that observation interval.
2. During replay, CHA associates A with an incident and creates a node matcher
   using A's interval. The incident ID may be recovered from historical mappings
   or newly assigned; that does not change this time calculation.
3. **Alert B on worker-1 starts at 11:20** and needs an assignment. Can it join
   through A's matcher?

Assume no other observations update this matcher before B is evaluated, and both
alerts pass the strategy's label and exclusion checks.

```text
Time             10:00             10:30             11:00       11:20 11:30
                 |                 |                 |           |     |
A in ALERTS      [===================================]
from: start      ^-----------------|                             x
from: end                                            ^-----------o-----|

^  Matcher's reference     |  Inclusive cutoff (reference + 30m)
o  B passes time check     x  B fails time check
```

| Strategy | Calculation for B | Result through this matcher |
|---|---|---|
| `from: start` | 11:20 − 10:00 = 80 minutes | Too late: the cutoff was 10:30. |
| `from: end` | 11:20 − 11:00 = 20 minutes | Within the window: the cutoff is 11:30. |

Passing the time check makes this matcher eligible, subject to
[candidate ranking](#ranking-eligible-candidates). The dashed lines show the
30-minute offset, not the entire eligible interval: overlapping observations can
also pass the `from: end` time check.

#### How live processing differs

Unlike historical replay, each live poll creates an observation with
**`start = end = processing timestamp`**. If A remains firing, the next poll observes
the same alert instance again; no new failure or reactivation is needed.

When a live observation selects a matcher, `from: start` moves its reference to
that timestamp; `from: end` advances its latest end if newer. Thus, neither uses
an alert's original firing or eventual resolution time during these updates.

Compare two configurations differing only in `from`, both with `within: 30m`.
Assume polls at 10:45 and 11:00 update the same matcher in each configuration,
with no further updates before B arrives at 11:20. Both configurations then have
the same reference and cutoff:

```text
Time             10:45             11:00             11:15 11:20       11:30
                 |                 |                 |     |           |
A live polls     *                 *
After 10:45 poll ^-----------------------------------|     x
After 11:00 poll                   ^-----------------------o-----------|

*  A observed firing (observation start = end = poll time)
^  Reference for either from: start or from: end
|  Inclusive cutoff (reference + 30m)
o  B at 11:20 passes time check     x  B at 11:20 would be too late
```

The 11:00 update moves the cutoff from 11:15 to 11:30, allowing B to pass the time
check. These poll times are illustrative, not a configured polling interval.
Historical replay can use observations with different start and end times, so it
does not necessarily produce the same windows as live polling.

Source: `MetricsIntervals`, `ProcessAlertsBatch`, `referenceTime`, and `update`
in [incidents.go](../pkg/processor/incidents.go).

#### The role of `within`

`within` limits how far an incoming observation's start can be from a **particular
matcher's reference time**. It is not a maximum alert or incident lifetime, and it
applies whether the incident still has firing alerts or not. The time check happens
**before** updating the matcher: an observation outside its window cannot renew it
to make itself eligible. A matcher selected and updated every 30 seconds can keep
its window moving indefinitely, but other matchers in the same incident may expire.

For example, using the default strategies:

1. Alert A creates an incident with exact and 15-minute temporal matchers.
2. A remains firing. Each live poll selects the exact matcher, updating its shared
   exact timeline but not the temporal matcher's reference.
3. If no observations update the temporal matcher, its window expires once more
   than 15 minutes have elapsed from its reference, even though A is still firing.
4. Alert B arrives with different labels. It cannot join through that expired
   temporal matcher merely because the incident remains active. Another eligible
   strategy could still allow it to join.

If all alerts resolve, matcher updates also stop; a later alert can reuse the
incident only through a still-eligible matcher or an applicable recovered
assignment. Resolution is therefore one reason updates stop, not a prerequisite
for `within` to matter. See `matches`, `bestMatch`, and `tryMatchIntervals` in
[incidents.go](../pkg/processor/incidents.go).

### Manifests and RBAC

To configure a deployment:

1. Choose the namespace using the [selection rules](#namespace-selection).
2. Save the example as `incident-rules.yaml`, adjusting its namespace and rules.
   Alternatively, start with the [shipped defaults](../manifests/backend/09_incident_rule_defaults.yaml).
3. Ensure the CHA ServiceAccount can `get`, `list`, and `watch` the named
   ConfigMap. The [RBAC manifest](../manifests/backend/10_incident_rule_rbac.yaml)
   provides these permissions for the repository deployment. The watcher uses a
   `metadata.name` field selector to restrict its list/watch requests.
4. Apply the configuration before starting CHA, or update it while CHA is running:

   ```sh
   oc apply -f incident-rules.yaml
   ```

5. Check CHA logs for validation errors and confirm that incident metrics reflect
   newly arriving and resolving alerts. Kubernetes accepting the ConfigMap does
   not mean CHA accepted the embedded rules.

The repository manifests use `openshift-cluster-health-analyzer`. For deployments
managed by cluster-observability-operator (COO), check the generated workload,
selected namespace, ConfigMap, and ServiceAccount permissions rather than assuming
the operator uses these manifests unchanged.

## Processing responsibilities

The following terms distinguish configuration from runtime state:

| Term | Meaning |
|---|---|
| Observation | An alert's labels and the time or interval when CHA observed it. |
| Assignment | An alert's membership in an incident, identified by `group_id`. |
| Matcher | Label selectors and timing state generated by a strategy. An incident can have several matchers. |
| Candidate | An incident considered for an incoming alert through one of its matchers. |
| Rules snapshot | A complete, validated configuration that does not change after publication. |

The processing flow is:

1. Watch the ConfigMap, validate its payload, and retain the latest valid rules
   snapshot.
2. At startup, load seven days of alert and incident-mapping history from the
   metrics backend.
3. Recover assignments supported by historical evidence, then replay observations
   chronologically to build matchers under the loaded rules.
4. During live processing, query firing alerts and evaluate them against available
   candidates. Recovered historical assignments take precedence when applicable.
5. Join a matching incident or create a new incident, then update the relevant
   matcher state.
6. Publish the resulting incident metrics and prune expired matcher state.

**Identity and grouping are different concerns.** Identity answers “which incident
was this alert assigned to?” Grouping answers “which incident can it join under
these rules?” A node-only selector can group alerts without identifying each one;
a time-only matcher has no labels at all. Preserving identity across
[rule changes](#non-retroactive-rule-changes) or
[restarts](#restart-and-identity-recovery) therefore requires evidence beyond the
grouping selectors. See the [implementation map](#implementation-map-and-open-details)
for the code behind each responsibility.

## Namespace selection

CHA selects the ConfigMap namespace in this order:

1. The nonempty, whitespace-trimmed `INCIDENT_RULES_NAMESPACE` environment variable.
2. The pod namespace read from
   `/var/run/secrets/kubernetes.io/serviceaccount/namespace`, if readable and nonempty.
3. `default`.

The kubeconfig context's namespace is **not** used. In a pod, putting the ConfigMap
in the pod's namespace normally requires no override. For local execution, set the
variable explicitly; the usual Kubernetes credentials and metrics-backend access
are still required:

```sh
INCIDENT_RULES_NAMESPACE=openshift-cluster-health-analyzer make run
```

For a deployment, the equivalent container environment entry is:

```yaml
env:
  - name: INCIDENT_RULES_NAMESPACE
    value: openshift-cluster-health-analyzer
```

Source: `watchNamespace` in [rules_watcher.go](../pkg/processor/rules_watcher.go).

## Validation and configuration lifecycle

CHA validates the entire payload before making it available to the processor.
It never activates only the valid rules from an invalid configuration.
Kubernetes validates the ConfigMap object, but CHA validates the embedded YAML.

Validation rejects unknown fields, multiple YAML documents, missing names or
strategies, unsupported or combined operators, empty or duplicate label keys,
invalid exclusion selectors, unsupported `from` values, and invalid durations.
Write seven days as `168h`, not `7d`. A duration of `168h` is valid; `0h` and
`168h1m` are not.

### Startup

When incident processing is enabled:

1. Select the namespace and start the ConfigMap watcher.
2. Wait up to 30 seconds for the watcher's initial cache synchronization.
3. Require a valid rules snapshot. Missing configuration, a missing `rules.yaml`
   key, invalid rules, or synchronization failure causes startup to fail.
4. Load history and reconstruct grouping state. A loading or initialization error
   also causes startup to fail; partially reconstructed state is not published.
5. Start live incident processing and serve metrics.

`--disable-incidents` skips this initialization. See
[server.go](../pkg/server/server.go) for startup wiring.

### Runtime

| ConfigMap event | Result |
|---|---|
| Valid, changed rules | Publish a new snapshot; the processor adopts it on a subsequent processing pass. |
| Metadata-only update or equivalent parsed rules | Keep the existing snapshot and matcher state. |
| Invalid payload or missing `rules.yaml` | Log the problem and keep the last valid snapshot. |
| ConfigMap deletion | Keep the last valid snapshot; deletion does not disable incidents. |
| ConfigMap recreated with valid rules | Resume accepting valid updates. |

The last valid snapshot is held in memory, not persisted as a backup. After a
restart, a valid ConfigMap is required again even if the previous process kept
working through an invalid update or deletion.

ConfigMap observation and processor adoption are separate events: the watcher log
alone does not prove that grouping has changed. The
[reload path](#reload-implementation-and-limits) describes how the processor adopts
the snapshot and rebuilds its matchers.

Invalid configuration updates do not themselves stop metric refreshes. Verify
freshness by observing an alert arrive and another resolve; a responding metrics
endpoint can still expose stale data if processing fails for another reason.

## Operators and candidate selection

### Operator semantics

| Operator | Behavior |
|---|---|
| `exact: true` | Create a full-label **subset selector**, not a label-set equality check. Incoming alerts must contain every selected label with the same value; additional labels are allowed. |
| `allOf: [keys]` | Create a selector only if every listed label is present. An incoming alert must match all selected values. |
| `anyOf: [keys]` | Create a separate alternative for each listed label that is present. Matching any one alternative is sufficient. If none are present, create no matcher. |
| `match: {}` | Match by time only, with no label selector. Temporal matchers are attached to newly created incidents, subject to the [Watchdog safeguard](#watchdog-safeguard). |

Selectors use literal label/value equality, not regular expressions. See
[LabelsSubsetMatcher](../pkg/common/labels.go) and matcher generation in
[rules.go](../pkg/processor/rules.go).

Exclusions apply both when generating matchers and when evaluating incoming alerts.
All pairs within one exclusion selector must match; matching any selector excludes
the alert from that strategy. Other strategies remain available. For example,
this strategy groups by alert name **or** namespace but excludes one monitoring
alert from that matching path:

```yaml
# One entry under a rule's strategies list.
- match:
    anyOf: [alertname, namespace]
  within: 24h
  from: start
  excludeAlerts:
    - alertname: Watchdog
      namespace: openshift-monitoring
```

### Mandatory `allOf` labels

With `allOf: [namespace, node]`, an allowed observation produces this selector:

```text
Alert:    {alertname="KubeNodeDown", namespace="openshift-monitoring", node="worker-1"}
Selector: {namespace="openshift-monitoring", node="worker-1"}
```

If `node` is absent, no matcher is generated for this strategy: it never reduces
to namespace-only matching or an empty, match-all selector. An empty label value
is different from an absent label; a present empty value is compared literally.
Other strategies remain available when `allOf` cannot match.

### Ranking eligible candidates

A candidate is eligible only if it passes label, exclusion, and time checks.
The incoming interval must not start before the matcher's initial start. Its
distance from the selected reference must be **at most** `within`; the boundary
is inclusive. Overlapping historical intervals can have a negative distance with
`from: end`, which does not by itself disqualify a match.

Among eligible candidates, compare these criteria in order:

1. Lower rule `priority`.
2. Rule `name` in lexical order.
3. Earlier position in that rule's `strategies` list.
4. Smaller temporal distance from the matcher's reference time.

Thus, an earlier time-only strategy can beat a later exact strategy; operator
names do not confer precedence. Selection uses a stable sort, but candidate input
order can depend on map iteration. Fully tied candidates do not have a guaranteed
global tie-breaker. Avoid relying on ties to select a particular incident.

Source: `matches` and `bestMatch` in [incidents.go](../pkg/processor/incidents.go).

### Default strategy order

The [default ConfigMap](../manifests/backend/09_incident_rule_defaults.yaml)
contains one rule with these strategies, in order:

| Order | Strategy | Window | Reference |
|---|---|---|---|
| 1 | Full-label `exact` selector | `24h` | Latest observed end |
| 2 | `allOf: [namespace, alertname, service, job, container]` | `24h` | Most recent matched start |
| 3 | `anyOf: [alertname, namespace]` | `24h` | Most recent matched start |
| 4 | Explicit time-only strategy | `15m` | Most recent matched start |
| 5 | Full-label `exact` fallback | `120h` | Latest observed end |

Strategy 3, also called fuzzy matching, excludes `Watchdog` and
`AlertmanagerReceiversNotConfigured` in `openshift-monitoring` from that strategy
only.

Within one rule, exact strategies with the same `from` and exclusions share the
timeline generated for an observation. Updating the first exact matcher therefore
keeps the exact fallback's end current. This sharing does not apply to every
strategy or across rules.

## Rules prevail over batching

A **batch** is the set of alerts processed together in one pass. Being in the same
batch is not a reason to share an incident. The grouping process is:

1. Try each observation against existing candidates, honoring any applicable
   recovered assignment first.
2. Collect observations that did not match.
3. Process those unmatched observations individually. Check candidates created
   earlier in this unmatched set before creating a new incident.
4. Add the generated matchers so subsequent observations and batches can use them.

With only `allOf: [node]` and no preexisting candidates:

| Inputs in one batch | Result |
|---|---|
| A on worker-1, B on worker-2 | Separate incidents |
| A on worker-1, C on worker-1 | One incident |

These simple partitions are unchanged by reversing the inputs or processing them
in separate batches at the same evaluation time. This is not a guarantee of
order independence for arbitrary competing rules. Explicit time-only strategies,
such as the default 15-minute strategy, can still group nearby alerts.

### Watchdog safeguard

`Watchdog` normally fires continuously to check the monitoring pipeline; seeing it
is not evidence of a new failure. After a restart or a gap in monitoring data, CHA
may see Watchdog and several unrelated alerts together. Their appearance in the
same poll does not mean their problems started together.

The safeguard avoids grouping those alerts **just because they appeared together**.
It applies only when Watchdog could not join an existing incident, as explained
below. CHA recognizes Watchdog by both `alertname="Watchdog"` and
`namespace="openshift-monitoring"`; the same name in another namespace does not
trigger the safeguard.

#### Example: alerts arriving together

Suppose CHA has no existing incidents or recovered assignments, and the only
configured strategy groups by time:

```yaml
strategies:
  - match: {}
    within: 15m
    from: start
```

Each row is a separate run, with no earlier incidents and all its alerts observed
together:

| Alerts in the batch | Result |
|---|---|
| A (a disk alert) and B (a network alert) | One incident: the time-only strategy allows B to join A. |
| Watchdog, A, and B | Three incidents: the safeguard prevents time-only grouping for all three alerts, not just Watchdog. |

A **temporal matcher** lets another alert join an incident based only on how close
it is in time. In the Watchdog case, CHA does not create these matchers for any of
the three incidents. That prevents time-only joins both within this batch and
later, while those matchers remain absent.

If label-based strategies are also configured, A and B can still share an
incident through those strategies. **The safeguard does not require every alert
to have its own incident.**

#### When the safeguard applies

1. CHA first tries to assign alerts to existing incidents using normal matching
   or recovered assignments.
2. It collects the alerts that still need an incident. These are the **unmatched
   alerts**.
3. If Watchdog is among them, CHA does not create temporal matchers for any new
   incidents created from that set. Label-based strategies still apply.

**Seeing Watchdog in a poll is not enough to trigger the safeguard.** If it already
joined an existing incident in step 1, this check does nothing for the remaining
alerts. Its position in the batch does not matter: CHA checks all unmatched alerts
before creating incidents.

#### What changed with configurable rules?

**The Watchdog protection already existed before rules.** The main grouping change
concerns batches where Watchdog does not trigger the safeguard:

| Situation | Before rules | With rules |
|---|---|---|
| Unmatched alerts without Watchdog | CHA put them in one shared incident simply because they were in the same batch. | They share an incident only if a configured strategy matches. |
| Unmatched alerts including Watchdog | No automatic shared incident; label-based grouping remained available, and new temporal matchers were made inactive. | Label-based grouping remains available, but new temporal matchers are not created at all. |

The safeguard still matters because an explicit `match: {}` strategy can reproduce
time-only grouping even though implicit batch grouping has been removed.
See the [pre-rules implementation](https://github.com/openshift/cluster-health-analyzer/blob/673e3176f442b8904c16a8586908460a7407438a/pkg/processor/incidents.go)
for the earlier behavior.

#### Limits and configurable exclusions

- **Existing matchers are unaffected.** Watchdog can join an existing incident
  through a temporal matcher if eligible. The safeguard neither deletes that
  matcher nor guarantees that Watchdog is isolated.
- **Rule reload and historical recovery are separate paths.** They can create
  temporal matchers for incidents containing Watchdog without running this check.
  The safeguard above applies to creating incidents from unmatched alerts, not
  every way CHA rebuilds its state.
- **Label exclusions are a different mechanism.** The defaults use `excludeAlerts`
  to keep Watchdog and `AlertmanagerReceiversNotConfigured` out of the
  `anyOf: [alertname, namespace]` strategy. Before rules, those exclusions were
  hardcoded. Keep them in custom rules if you want that behavior. Removing them
  does not turn off the built-in batch safeguard, which is triggered only by
  Watchdog.

Code: `watchdogAlert`, `addIntervalsGroups`, `newRootGroup`, and `forcedRoot` in
[incidents.go](../pkg/processor/incidents.go); `adoptRules` in
[processor.go](../pkg/processor/processor.go). Tests cover temporal-matcher
suppression and exclusions in [rules_schema_test.go](../pkg/processor/rules_schema_test.go),
and label-based grouping with Watchdog in [incidents_test.go](../pkg/processor/incidents_test.go).

## Non-retroactive rule changes

The intended behavior is to preserve existing assignments while using new rules
to assign alerts that do not yet have an incident. “New” means **unassigned**, not
merely first observed after the configuration update. An existing incident may
accept new members; a rule edit is not a request to split or merge historical
incidents.

For example, assume A and B are firing and share an incident from namespace-based
rules. After changing to node-only rules, with observations inside the window:

| Alert | Namespace | Node | Before update | Intended result |
|---|---|---|---|---|
| A | production | worker-1 | ID-123 | ID-123 |
| B | production | worker-2 | ID-123 | ID-123 |
| New C | production | worker-1 | Unassigned | May join ID-123 |
| New D | production | worker-3 | Unassigned | New incident if no other candidate matches |

### Reload implementation and limits

The live reload path currently:

1. Reads the latest valid rules snapshot before loading the next live alert batch.
2. Creates a replacement collection containing the new rules and matchers generated
   from the most recently processed firing alerts, with their existing incident IDs.
3. Swaps in the completed collection. Preparation errors leave the old collection
   intact; a pass never combines new rules with old-rule matchers.

This supports the simple node-based example, but it is **not a general identity
preservation guarantee**:

- **No historical rebuild:** resolved alerts are absent, and increasing a window
  or introducing label keys does not reload older observations. A re-firing alert
  may receive a new incident ID.
- **Reset timing:** new matcher references use the reload time, not the original
  observation times.
- **Matcher-dependent identity:** there is no independent live assignment lookup.
  Missing required labels or competing matchers can prevent reuse of an old ID.

These are implementation gaps, not configuration options. Full preservation needs
identity independent of matching and sufficient observations within the history
horizon. See `recordAssignments` and `adoptRules` in
[processor.go](../pkg/processor/processor.go), and the reload tests in
[processor_test.go](../pkg/processor/processor_test.go).

## Restart and identity recovery

Restart reconstruction uses `ALERTS` firing history and
`cluster_health_components_map` assignment history; it does not use a separate
incident database.

Initialization proceeds as follows:

1. Query both histories for the last seven days, with a one-minute query step.
2. Reconstruct alert observation intervals from the samples.
3. Compare complete historical alert labels with the source-label selectors in
   incident mappings. Correlation checks mapping times around the alert interval's
   end, with a ten-minute tolerance.
4. Recover an assignment only when the evidence identifies exactly one incident.
5. Replay observations in chronological order. A recovered assignment takes
   precedence over rule matching for the corresponding labels and time range.
   Other observations use the configured rules.
6. Reconcile remaining matcher IDs against historical mappings. IDs established
   by recovered assignments are exempt from this remapping.
7. Install the completed grouping collection and resume live processing.

Source: `InitGroupsCollection` in [processor.go](../pkg/processor/processor.go),
and `correlateHistoricalAssignments`, `forcedAssignment`, and `UpdateGroupUUIDs`
in [incidents.go](../pkg/processor/incidents.go).

### Recovery evidence is lossy

The exported metric retains namespace, alert name, severity, and labels used for
component matching, not every original alert label. For example, `node` may be
absent. See `getLabelsSubset` in [types.go](../pkg/processor/types.go).

If two simultaneous alerts have identical exported source labels but belong to
different incidents, neither receives a forced assignment. Normal grouping and
subsequent matcher-ID reconciliation apply, so **original IDs are not guaranteed**.
Missing or expired history also prevents reliable recovery; a longer query horizon
cannot recreate data the backend no longer has.

**Both limitations predate configurable rules.** The earlier implementation exported
the same restricted labels and selected the first matching historical incident
without checking uniqueness. The new correlation step adds unambiguous forced
assignments before that existing reconciliation path; it does not eliminate all
ambiguity. Custom rules can expose the problem by separating alerts using a label
such as `node` that their mappings omit. See `correlateHistoricalAssignments` and
`previousIncidentsMatcher.match` in [incidents.go](../pkg/processor/incidents.go).

`TestRestartAfterRuleChangePreservesAssignments` in
[processor_test.go](../pkg/processor/processor_test.go) covers recovery of ID-123
for the [rule-change example](#non-retroactive-rule-changes) with unambiguous history.
It does not establish recovery for ambiguous mappings or arbitrary label changes.

Chronological replay matters: preloading matchers from later, disjoint
observations could change which candidates were eligible for an earlier alert.

## Seven-day horizon and pruning

Three different time concepts apply:

| Concept | Behavior |
|---|---|
| Matching window | Each strategy requires `0 < within <= 168h`; the upper boundary is inclusive. |
| Historical query horizon | Startup requests the last seven days. Live reload currently does not repeat this query. |
| Incident lifetime | Not capped at seven days; recent observations can keep an older incident eligible. |

[Time-reference examples](#understanding-from-start-and-from-end) explain how
windows move; [candidate selection](#ranking-eligible-candidates) defines the
eligibility checks. Retention below determines when matcher state is removed,
not whether an incoming alert may match it.

### Retention and pruning

Pruning removes old matchers from memory. Retention is measured from the same
strategy-specific reference used for matching:

- Exact strategies retain state for `max(120h, within)`.
- Other strategies retain state for `max(24h, within)`.
- State is removed only after that retention period has elapsed.

Retained state does not extend matching beyond `within`. Using the end reference
for end-anchored strategies prevents an old start timestamp from discarding a
matcher whose end is still recent. See retention setup in
[rules.go](../pkg/processor/rules.go) and `PruneGroups` in
[incidents.go](../pkg/processor/incidents.go).

### Recovery example

Assume a `168h` node strategy and sufficient, unambiguous history:

1. Six days ago, alert A on worker-1 created ID-123 and then resolved.
2. Today, alert B on worker-1 arrives within the strategy's window.
3. An uninterrupted process can match B to ID-123 using the retained candidate.
4. After a restart, seven-day loading can recover ID-123 and reconstruct its
   candidate, allowing B to join it as well.

An incident created ten days ago can similarly retain its ID through recent
observations. Its original creation need not fall inside the query horizon.
The startup horizon is defined in [server.go](../pkg/server/server.go).

## Verification and compatibility

### Automated coverage

The following are existing tests, not a claim that every lifecycle scenario is
covered or that cluster-level verification has passed:

| Area | Test references |
|---|---|
| Parsing, exclusions, priority, windows, shared exact timelines | `TestParseRuleValidation`, `TestExcludedAlertsCannotMatchExistingFuzzyMatcher`, `TestRulePriorityPrecedesStrategyOrder`, `TestRuleWindowConfiguration`, `TestRuleWindowMaximum`, `TestExactFallbackKeepsLatestObservation` in [rules_schema_test.go](../pkg/processor/rules_schema_test.go) |
| Mandatory labels and batch grouping | `TestAllOfRequiresEveryLabel`, `TestRulesOverrideInitialBatchGrouping` in [rules_test.go](../pkg/processor/rules_test.go) |
| Last-valid configuration and namespace selection | `TestRulesWatcherStartRequiresValidRules`, `TestRulesWatcherRebuildInvalidatesChangedDefinitions`, `TestRulesWatcherObservesUpdatesAndDeletion`, `TestRulesNamespacePrecedence` in [rules_watcher_test.go](../pkg/processor/rules_watcher_test.go) |
| Reload and historical identity | `TestProcessorReloadsMatchersOnRuleChange`, `TestProcessorFailedReloadDoesNotPublishPartialState`, `TestRestartAfterRuleChangePreservesAssignments`, `TestHistoricalAssignmentCorrelation` in [processor_test.go](../pkg/processor/processor_test.go) |
| Startup failure without rules | `TestStartServerFailsBeforeServingWithoutRules` in [server_test.go](../pkg/server/server_test.go) |
| Strategy-aware pruning | `TestPruningRespectsStrategyReference` in [rules_test.go](../pkg/processor/rules_test.go), `TestMatcherRetentionUsesStrategyReference` in [rules_schema_test.go](../pkg/processor/rules_schema_test.go) |

Coverage still needs to address the [known limitations](#known-limitations), rapid
successive updates, and loader failures. Isolate strategies so a fallback cannot
hide a failed condition, and test before/at/after time boundaries and matcher-update
interleavings.

### Configuration lifecycle in a real cluster

Use a dedicated test instance with controlled history and non-notifying alert
routing. Generate alerts through the existing Prometheus test source, not a new
injection API. Then:

1. Apply valid rules V1 and record incident IDs for known alerts.
2. Apply an invalid update. Verify rejection in the logs, introduce one alert,
   and resolve another. Confirm metrics continue refreshing under V1.
3. Apply valid V2 with observably different grouping. Verify new unassigned alerts
   use V2, and check existing IDs against the preservation limits above.
4. Delete the ConfigMap. Introduce and resolve alerts again; processing should
   continue with V2.
5. Recreate the ConfigMap with valid V3. Verify new assignments demonstrate V3
   adoption rather than continued use of V2.
6. Test a metadata-only update: it should not reset matcher state.
7. Restart with valid rules and populated history, including a restart after a
   rule change. Check IDs, not only group counts. Separately test missing or
   invalid initial configuration and history-loading failures.
8. Restore the instance's configuration and test resources on success or failure.

Never run this lifecycle sequence against production or a concurrently shared
backend. Readiness and HTTP success are not sufficient checks of metric freshness.
See the [integration guide](../test/integration/README.md) for setup and cleanup.

### Compatibility verification

Legacy comparison tests in [rules_test.go](../pkg/processor/rules_test.go) compare
**normalized partitions**: which alerts share an incident, independently of random
UUID values. Use partition comparisons for grouping equivalence, but compare
actual IDs when testing identity preservation. Investigate unexpected differences
rather than broadly excluding them from comparisons.

Keep the standard smoke/TLS/crashloop suite and the five long grouping scenarios:
time proximity, fuzzy namespace, fuzzy alert name, exact replay, and severity-change
subset matching. They exercise default behavior, not all custom configurations,
reloads, or multi-day boundaries. Unit tests can advance time without waiting days.

### Commands

From the repository root:

```sh
# Focused tests.
go test ./pkg/processor ./pkg/server

# Full race-enabled unit suite and Go linter.
make test
make lint

# Compile integration packages without contacting a cluster.
go test ./test/integration/... -run '^$'
```

For cluster execution, follow the [integration guide](../test/integration/README.md)
and record which checks ran or were skipped.

## Implementation map and open details

| Responsibility | Source |
|---|---|
| Schema validation, matcher generation, shared exact timelines | [rules.go](../pkg/processor/rules.go) |
| Namespace selection, ConfigMap watch, last-valid snapshot | [rules_watcher.go](../pkg/processor/rules_watcher.go) |
| Batch grouping, selection, retention, historical ID correlation | [incidents.go](../pkg/processor/incidents.go) |
| Alert-to-component mapping and exported source-label selection | [alerts.go](../pkg/processor/alerts.go), [types.go](../pkg/processor/types.go) |
| Live reload, historical loading, metric updates | [processor.go](../pkg/processor/processor.go) |
| Startup and history horizon | [server.go](../pkg/server/server.go) |
| Default configuration and permissions | [ConfigMap](../manifests/backend/09_incident_rule_defaults.yaml), [RBAC](../manifests/backend/10_incident_rule_rbac.yaml) |
| Tests | [Processor unit tests](../pkg/processor), [integration guide](../test/integration/README.md) |

Open work is summarized under [known limitations](#known-limitations), with details
in the linked reload, recovery, selection, and verification sections.
