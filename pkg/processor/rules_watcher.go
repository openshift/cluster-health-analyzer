package processor

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

const (
	// DefaultRulesConfigMapName is the ConfigMap holding the shipped default
	// rules. It is owned and reconciled by the deploying operator.
	DefaultRulesConfigMapName = "cluster-health-analyzer-default-rules"
	// CustomRulesConfigMapName is the user-owned ConfigMap holding additional
	// or overriding rules. It may be absent.
	CustomRulesConfigMapName = "cluster-health-analyzer-custom-rules"
	// RulesConfigKey is the ConfigMap data key holding the rules section.
	RulesConfigKey = "rules.yaml"
)

type RulesWatcher struct {
	mu        sync.RWMutex
	snapshot  *RulesSnapshot
	informers map[string]cache.SharedIndexInformer
}

// watchedConfigMaps lists the ConfigMaps whose rules are merged into one
// snapshot, in merge order: a custom rule replaces a default rule with the
// same name.
func watchedConfigMaps() []string {
	return []string{DefaultRulesConfigMapName, CustomRulesConfigMapName}
}

func NewRulesWatcher(config *rest.Config) (*RulesWatcher, error) {
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return newRulesWatcher(client)
}

func newRulesWatcher(client kubernetes.Interface) (*RulesWatcher, error) {
	watcher := &RulesWatcher{informers: map[string]cache.SharedIndexInformer{}}
	for _, name := range watchedConfigMaps() {
		// Kubernetes field selectors cannot combine two names in one
		// request, so each ConfigMap gets its own informer.
		factory := informers.NewSharedInformerFactoryWithOptions(client, 0,
			informers.WithNamespace(watchNamespace()),
			informers.WithTweakListOptions(func(opts *metav1.ListOptions) {
				opts.FieldSelector = "metadata.name=" + name
			}),
		)
		informer := factory.Core().V1().ConfigMaps().Informer()
		_, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    func(_ interface{}) { watcher.rebuild() },
			UpdateFunc: func(_, _ interface{}) { watcher.rebuild() },
			DeleteFunc: func(_ interface{}) { watcher.rebuild() },
		})
		if err != nil {
			return nil, err
		}
		watcher.informers[name] = informer
	}
	return watcher, nil
}

// watchNamespace returns the namespace holding the rules ConfigMaps.
// An explicit environment override wins, so local runs and operators can
// point at a namespace other than the pod's own.
func watchNamespace() string {
	if ns := strings.TrimSpace(os.Getenv("INCIDENT_RULES_NAMESPACE")); ns != "" {
		return ns
	}
	if ns, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil {
		if trimmed := strings.TrimSpace(string(ns)); trimmed != "" {
			return trimmed
		}
	}
	return "default"
}

// Start waits for the initial list before allowing incident processing to begin.
func (rw *RulesWatcher) Start(ctx context.Context) error {
	synced := make([]cache.InformerSynced, 0, len(rw.informers))
	for _, informer := range rw.informers {
		go informer.Run(ctx.Done())
		synced = append(synced, informer.HasSynced)
	}
	syncContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if !cache.WaitForCacheSync(syncContext.Done(), synced...) {
		return fmt.Errorf("failed to synchronize incident rules config")
	}
	rw.rebuild()
	// The defaults ConfigMap must be valid for CHA to start; the custom one
	// is optional.
	if rw.loadRules(DefaultRulesConfigMapName) == nil {
		return fmt.Errorf("no valid default IncidentRules in %s", DefaultRulesConfigMapName)
	}
	if snapshot := rw.GetSnapshot(); snapshot == nil || len(snapshot.rules) == 0 {
		return fmt.Errorf("no valid IncidentRules loaded")
	}
	return nil
}

func (rw *RulesWatcher) GetSnapshot() *RulesSnapshot {
	rw.mu.RLock()
	defer rw.mu.RUnlock()
	return rw.snapshot
}

func (rw *RulesWatcher) rebuild() {
	rw.mu.Lock()
	defer rw.mu.Unlock()

	var rules []*parsedRule
	for _, name := range watchedConfigMaps() {
		rules = append(rules, rw.loadRules(name)...)
	}
	if rules == nil {
		// No ConfigMap yielded valid rules. Keep the last valid snapshot so
		// incident processing continues; startup without any valid snapshot
		// still fails in Start.
		if rw.snapshot != nil {
			slog.Warn("Keeping last valid IncidentRules: no valid configuration available")
		}
		return
	}
	rules = mergeRules(rules)
	sortRules(rules)
	snapshot := &RulesSnapshot{rules: rules}
	// Metadata-only updates must not invalidate stateful matchers.
	if !reflect.DeepEqual(rw.snapshot, snapshot) {
		rw.snapshot = snapshot
		slog.Info("Loaded IncidentRules", "count", len(rules))
	}
}

// loadRules reads one ConfigMap from the watcher's store and parses its
// rules. A missing, incomplete or invalid ConfigMap is logged and yields no
// rules; it does not invalidate rules loaded from the other ConfigMap.
func (rw *RulesWatcher) loadRules(name string) []*parsedRule {
	item, exists, err := rw.informers[name].GetStore().GetByKey(watchNamespace() + "/" + name)
	if err != nil {
		slog.Error("Cannot load incident rules", "configmap", name, "error", err)
		return nil
	}
	if !exists {
		// The custom ConfigMap is user-owned and may legitimately be absent.
		return nil
	}
	cm, ok := item.(*corev1.ConfigMap)
	if !ok {
		slog.Error("Cannot load incident rules: unexpected object type", "configmap", name)
		return nil
	}
	data, found := cm.Data[RulesConfigKey]
	if !found {
		slog.Error("Cannot load incident rules: missing key in config", "configmap", name, "key", RulesConfigKey)
		return nil
	}
	parsed, err := parseRulesConfig([]byte(data))
	if err != nil {
		slog.Error("Cannot load invalid incident rules", "configmap", name, "error", err)
		return nil
	}
	return parsed
}

// mergeRules keeps the last rule for each name, so a custom rule replaces a
// default rule with the same name.
func mergeRules(rules []*parsedRule) []*parsedRule {
	indexByName := make(map[string]int, len(rules))
	merged := make([]*parsedRule, 0, len(rules))
	for _, rule := range rules {
		if i, ok := indexByName[rule.Name]; ok {
			merged[i] = rule
			continue
		}
		indexByName[rule.Name] = len(merged)
		merged = append(merged, rule)
	}
	return merged
}

func sortRules(rules []*parsedRule) {
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].Priority != rules[j].Priority {
			return rules[i].Priority < rules[j].Priority
		}
		return rules[i].Name < rules[j].Name
	})
}
