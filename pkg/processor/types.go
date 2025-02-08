package processor

// This file contains support types used by the processor.

import (
	"hash/fnv"
	"regexp"
	"slices"
	"strings"
)

// # Component Health Map

// ComponentHealthMap represents mapping of a specific source to the component's health.
type ComponentHealthMap struct {
	Layer     string            // Layer of the component
	Component string            // Component name
	SrcType   SrcType           // Type of the source (alert, cluster_operator_condition)
	SrcLabels map[string]string // Identifying labels of the source
	GroupId   string            // Group ID of the component
	Health    HealthValue       // Health value of the component
}

// SrcType represents the type of the source.
type SrcType string

const (
	Alert                    SrcType = "alert"
	ClusterOperatorCondition SrcType = "cluster_operator_condition"
)

// HealthValue represents the health value of the component.
type HealthValue int

const (
	Healthy  HealthValue = 0
	Warning  HealthValue = 1
	Critical HealthValue = 2

	SrcLabelPrefix = "src_"
)

func (h HealthValue) String() string {
	switch h {
	case Healthy:
		return "info"
	case Warning:
		return "warning"
	case Critical:
		return "critical"
	default:
		return "none"
	}
}
func ParseHealthValue(s string) HealthValue {
	switch strings.ToLower(s) {
	case "info":
		return Healthy
	case "warning":
		return Warning
	case "critical":
		return Critical
	default:
		// We don't recognize the severity, so we'll default to warning
		return Warning
	}
}

// hashLabelValues returns a hash of the labels of the component.
//
// This is used to uniquely identify the component when deduplicating.
func (c ComponentHealthMap) hashLabelValues() uint64 {
	h := fnv.New64a()
	labels := c.Labels()
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	for _, k := range keys {
		v := labels[k]
		h.Write([]byte(k))
		h.Write([]byte(v))
	}
	return h.Sum64()
}

// Labels to be exported for the mapping.
func (c ComponentHealthMap) Labels() map[string]string {
	metaLabels := map[string]string{
		"layer":     c.Layer,
		"component": c.Component,
		"type":      string(c.SrcType),
		"group_id":  c.GroupId,
	}

	labels := make(map[string]string, len(c.SrcLabels)+len(metaLabels))
	for k, v := range metaLabels {
		labels[k] = v
	}

	for k, v := range c.SrcLabels {
		labels[SrcLabelPrefix+k] = v
	}
	return labels
}

// # Component Matcher

// componentMatcher represents a matcher definition for a component.
//
// It matches if any of the label matchers match the labels.
type componentMatcher struct {
	component string
	matchers  []LabelsMatcher
}

type LabelsMatcher interface {
	Matches(labels map[string]string) (match bool, keys []string)
	Equals(other LabelsMatcher) bool
}

// labelMatcher represents a matcher definition for a label.
type labelMatcher struct {
	key     string
	matcher ValueMatcher
}

// Matches implements the LabelsMatcher interface.
func (l labelMatcher) Matches(labels map[string]string) (bool, []string) {
	if l.matcher.Matches(labels[l.key]) {
		return true, []string{l.key}
	}
	return false, nil
}

// Equals implements the LabelsMatcher interface.
func (l labelMatcher) Equals(other LabelsMatcher) bool {
	ol, ok := other.(labelMatcher)
	if !ok {
		return false
	}
	return l.key == ol.key && l.matcher.Equals(ol.matcher)
}

// labelsMatcher represents a matcher definition for a set of labels.
// It matches if all of the label matchers match the labels.
type labelsSubsetMatcher struct {
	Labels map[string]string
}

func (l labelsSubsetMatcher) Matches(labels map[string]string) (bool, []string) {
	var keys []string
	for k, m := range l.Labels {
		if v, ok := labels[k]; !ok || v != m {
			return false, nil
		}
		keys = append(keys, k)
	}
	return true, keys
}

func (l labelsSubsetMatcher) Equals(other LabelsMatcher) bool {
	o, ok := other.(labelsSubsetMatcher)
	if !ok {
		return false
	}

	if len(l.Labels) != len(o.Labels) {
		return false
	}

	// The length of the maps is the same, so if it's subset of the other, it's equal.
	ret, _ := l.Matches(o.Labels)
	return ret
}

// ValueMatcher represents a matcher for a specific value.
//
// Multiple implementations are provided for different types of matchers.
type ValueMatcher interface {
	Matches(value string) bool
	Equals(other ValueMatcher) bool
}

// stringMatcher is a matcher for a list of strings.
//
// It matches if the value is in the list of strings.
type stringMatcher []string

func (s stringMatcher) Matches(value string) bool {
	for _, v := range s {
		if v == value {
			return true
		}
	}
	return false
}

func equalsNoOrder(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	seen := make(map[string]int, len(a))
	for _, v := range a {
		seen[v]++
	}
	for _, v := range b {
		if seen[v] == 0 {
			return false
		}
		seen[v]--
	}
	return true
}

// Equals implements the ValueMatcher interface.
func (s stringMatcher) Equals(other ValueMatcher) bool {
	o, ok := other.(stringMatcher)
	if !ok {
		return false
	}
	return equalsNoOrder(s, o)
}

// regexpMatcher is a matcher for a list of regular expressions.
//
// It matches if the value matches any of the regular expressions.
type regexpMatcher []*regexp.Regexp

func (r regexpMatcher) Matches(value string) bool {
	for _, re := range r {
		if re.MatchString(value) {
			return true
		}
	}
	return false
}

// Equals implements the ValueMatcher interface.
func (r regexpMatcher) Equals(other ValueMatcher) bool {
	o, ok := other.(regexpMatcher)
	if !ok {
		return false
	}
	s1 := make([]string, 0, len(r))
	for _, re := range r {
		s1 = append(s1, re.String())
	}
	s2 := make([]string, 0, len(o))
	for _, re := range o {
		s2 = append(s2, re.String())
	}
	return equalsNoOrder(s1, s2)
}

// findComponent tries to dtermine a component for given labels using the provided matchers.
//
// It returns the component and the keys that matched.
// If no match is found, it returns an empty component and nil keys.
func findComponent(compMatchers []componentMatcher, labels map[string]string) (
	component string, keys []string) {
	for _, compMatcher := range compMatchers {
		for _, labelsMatcher := range compMatcher.matchers {
			if matches, keys := labelsMatcher.Matches(labels); matches {
				return compMatcher.component, keys
			}
		}
	}
	return "", nil
}

// componentMatcherFn is a function that tries matching provided labels to a component.
// It returns the layer, component and the keys from the labels that were used for matching.
// If no match is found, it returns an empty layer, component and nil keys.
type componentMatcherFn func(labels map[string]string) (layer, comp string, keys []string)

func evalMatcherFns(fns []componentMatcherFn, labels map[string]string) (
	layer, comp string, labelsSubset map[string]string) {
	for _, fn := range fns {
		if layer, comp, keys := fn(labels); layer != "" {
			return layer, comp, getLabelsSubset(labels, keys...)
		}
	}
	return "Others", "Others", getLabelsSubset(labels)
}

// getLabelsSubset returns a subset of the labels with given keys.
func getLabelsSubset(m map[string]string, keys ...string) map[string]string {
	keys = append([]string{"namespace", "alertname", "severity"}, keys...)
	return getMapSubset(m, keys...)
}

// getLabelsSubset returns a subset of the labels with given keys.
func getMapSubset(m map[string]string, keys ...string) map[string]string {
	subset := make(map[string]string, len(keys))
	for _, key := range keys {
		if val, ok := m[key]; ok {
			subset[key] = val
		}
	}
	return subset
}
