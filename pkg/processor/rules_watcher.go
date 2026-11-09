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
	// ConfigMapName is the analyzer configuration ConfigMap holding the rules.
	ConfigMapName = "cluster-health-analyzer-config"
	// RulesConfigKey is the ConfigMap data key holding the rules section.
	RulesConfigKey = "rules.yaml"
)

type RulesWatcher struct {
	mu       sync.RWMutex
	snapshot *RulesSnapshot
	informer cache.SharedIndexInformer
}

func NewRulesWatcher(config *rest.Config) (*RulesWatcher, error) {
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return newRulesWatcher(client)
}

func newRulesWatcher(client kubernetes.Interface) (*RulesWatcher, error) {
	factory := informers.NewSharedInformerFactoryWithOptions(client, 0,
		informers.WithNamespace(watchNamespace()),
		informers.WithTweakListOptions(func(opts *metav1.ListOptions) {
			opts.FieldSelector = "metadata.name=" + ConfigMapName
		}),
	)
	watcher := &RulesWatcher{informer: factory.Core().V1().ConfigMaps().Informer()}
	_, err := watcher.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(_ interface{}) { watcher.rebuild() },
		UpdateFunc: func(_, _ interface{}) { watcher.rebuild() },
		DeleteFunc: func(_ interface{}) { watcher.rebuild() },
	})
	if err != nil {
		return nil, err
	}
	return watcher, nil
}

// watchNamespace returns the namespace holding the rules ConfigMap.
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
	go rw.informer.Run(ctx.Done())
	syncContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if !cache.WaitForCacheSync(syncContext.Done(), rw.informer.HasSynced) {
		return fmt.Errorf("failed to synchronize incident rules config")
	}
	rw.rebuild()
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
	for _, item := range rw.informer.GetStore().List() {
		cm, ok := item.(*corev1.ConfigMap)
		if !ok {
			slog.Error("Cannot load incident rules: unexpected object type")
			continue
		}
		data, found := cm.Data[RulesConfigKey]
		if !found {
			slog.Error("Cannot load incident rules: missing key in config", "configmap", ConfigMapName, "key", RulesConfigKey)
			continue
		}
		parsed, err := parseRulesConfig([]byte(data))
		if err != nil {
			slog.Error("Cannot load invalid incident rules", "configmap", ConfigMapName, "error", err)
			continue
		}
		rules = parsed
	}
	if rules == nil {
		// The ConfigMap is missing, deleted or invalid. Keep the last valid
		// snapshot so incident processing continues; startup without any
		// valid snapshot still fails in Start.
		if rw.snapshot != nil {
			slog.Warn("Keeping last valid IncidentRules: no valid configuration available", "configmap", ConfigMapName)
		}
		return
	}
	sortRules(rules)
	snapshot := &RulesSnapshot{rules: rules}
	// Metadata-only updates must not invalidate stateful matchers.
	if !reflect.DeepEqual(rw.snapshot, snapshot) {
		rw.snapshot = snapshot
		slog.Info("Loaded IncidentRules", "count", len(rules))
	}
}

func sortRules(rules []*parsedRule) {
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].Priority != rules[j].Priority {
			return rules[i].Priority < rules[j].Priority
		}
		return rules[i].Name < rules[j].Name
	})
}
