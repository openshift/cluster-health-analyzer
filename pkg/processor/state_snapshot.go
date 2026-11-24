package processor

import (
	"time"

	"github.com/prometheus/common/model"

	"github.com/openshift/cluster-health-analyzer/pkg/common"
)

// MatcherState is a JSON-serializable deep copy of the matcher state held in
// memory by a running processor. It exists to debug the matcher lifecycle
// (creation, window movement, pruning, reload regeneration); the processor
// itself never consumes it.
type MatcherState struct {
	Groups      []MatcherGroupState `json:"groups"`
	Assignments []AssignmentState   `json:"assignments"`
	ForcedRoots []MatcherGroupState `json:"forced_roots"`
}

// MatcherGroupState describes one matcher of one incident.
type MatcherGroupState struct {
	GroupID       string              `json:"group_id"`
	RootGroupID   string              `json:"root_group_id"`
	Strategy      StrategyState       `json:"strategy"`
	Start         time.Time           `json:"start"`
	Modified      time.Time           `json:"modified"`
	End           time.Time           `json:"end"`
	ReferenceTime time.Time           `json:"reference_time"`
	Selectors     []map[string]string `json:"selectors,omitempty"`
}

// StrategyState exposes the parts of a parsed strategy that explain a
// matcher's behavior.
type StrategyState struct {
	RuleName  string   `json:"rule_name"`
	Priority  int32    `json:"priority"`
	Operator  string   `json:"operator,omitempty"`
	Keys      []string `json:"keys,omitempty"`
	Within    string   `json:"within"`
	Retention string   `json:"retention"`
	From      string   `json:"from"`
	Expand    bool     `json:"expand"`
}

// AssignmentState describes one recovered alert-to-incident assignment.
type AssignmentState struct {
	Labels     map[string]string `json:"labels"`
	Start      time.Time         `json:"start"`
	End        time.Time         `json:"end"`
	IncidentID string            `json:"incident_id"`
}

// MatcherStateSnapshotter is implemented by the processor so that debug
// endpoints can request the in-memory matcher state without depending on
// the unexported processor type.
type MatcherStateSnapshotter interface {
	MatcherStateSnapshot() MatcherState
}

// MatcherStateSnapshot returns a deep copy of the current matcher state.
// It is safe to call concurrently with processing.
func (p *processor) MatcherStateSnapshot() MatcherState {
	p.stateMu.RLock()
	defer p.stateMu.RUnlock()
	if p.groupsCollection == nil {
		return MatcherState{}
	}
	return p.groupsCollection.stateSnapshot()
}

func (gc *GroupsCollection) stateSnapshot() MatcherState {
	ret := MatcherState{
		Groups:      make([]MatcherGroupState, 0, len(gc.Groups)),
		Assignments: make([]AssignmentState, 0, len(gc.assignments)),
		ForcedRoots: make([]MatcherGroupState, 0, len(gc.forcedRoots)),
	}
	for _, g := range gc.Groups {
		ret.Groups = append(ret.Groups, groupState(g))
	}
	for _, a := range gc.assignments {
		ret.Assignments = append(ret.Assignments, AssignmentState{
			Labels:     labelsToStrings(a.labels),
			Start:      a.start.Time(),
			End:        a.end.Time(),
			IncidentID: a.id,
		})
	}
	for _, root := range gc.forcedRoots {
		s := groupState(root)
		if root.strategy == nil {
			// Forced roots are synthetic anchors without a strategy; report
			// which rule created the matchers of their incident.
			for _, g := range gc.Groups {
				if g.RootGroupID == root.RootGroupID && g.strategy != nil {
					s.Strategy.RuleName = g.strategy.RuleName
					break
				}
			}
		}
		ret.ForcedRoots = append(ret.ForcedRoots, s)
	}
	return ret
}

func groupState(g *GroupMatcher) MatcherGroupState {
	ret := MatcherGroupState{
		GroupID:     g.GroupID,
		RootGroupID: g.RootGroupID,
		Start:       g.Start.Time(),
		Modified:    g.Modified.Time(),
		End:         g.End.Time(),
	}
	if g.strategy != nil {
		ret.ReferenceTime = g.referenceTime().Time()
		ret.Strategy = StrategyState{
			RuleName:  g.strategy.RuleName,
			Priority:  g.strategy.Priority,
			Operator:  g.strategy.operator,
			Within:    g.strategy.Within.String(),
			Retention: g.strategy.Retention.String(),
			From:      g.strategy.From,
			Expand:    g.strategy.Expand,
		}
		for _, k := range g.strategy.Keys {
			ret.Strategy.Keys = append(ret.Strategy.Keys, string(k))
		}
	}
	for _, m := range g.Matchers {
		ret.Selectors = append(ret.Selectors, labelsToStrings(labelsOf(m)))
	}
	return ret
}

func labelsOf(m common.LabelsMatcher) model.LabelSet {
	if s, ok := m.(common.LabelsSubsetMatcher); ok {
		return s.Labels
	}
	return nil
}

func labelsToStrings(labels model.LabelSet) map[string]string {
	if labels == nil {
		return nil
	}
	ret := make(map[string]string, len(labels))
	for k, v := range labels {
		ret[string(k)] = string(v)
	}
	return ret
}
