package processor

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func testRulesYAML(name, strategiesYAML string) string {
	return "rules:\n- name: " + name + "\n  strategies:\n" + strategiesYAML
}

const testNodeStrategy = "  - match:\n      allOf: [node]\n    within: 24h\n    from: start\n"

func testConfigMap(t *testing.T, rulesYAML string) *corev1.ConfigMap {
	t.Helper()
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: ConfigMapName, Namespace: watchNamespace()},
		Data:       map[string]string{RulesConfigKey: rulesYAML},
	}
}

func testRulesWatcher(t *testing.T, objects ...runtime.Object) (*RulesWatcher, *fake.Clientset) {
	t.Helper()
	client := fake.NewSimpleClientset(objects...)
	watcher, err := newRulesWatcher(client)
	require.NoError(t, err)
	return watcher, client
}

func TestRulesWatcherStartRequiresValidRules(t *testing.T) {
	for name, rulesYAML := range map[string]string{
		"missing": "",
		"invalid": testRulesYAML("invalid", "  - match:\n      exact: true\n      allOf: [node]\n    within: 24h\n    from: end\n"),
		"valid":   testRulesYAML("custom", testNodeStrategy),
	} {
		t.Run(name, func(t *testing.T) {
			var objects []runtime.Object
			if rulesYAML != "" {
				objects = append(objects, testConfigMap(t, rulesYAML))
			}
			watcher, _ := testRulesWatcher(t, objects...)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := watcher.Start(ctx)
			if name == "valid" {
				require.NoError(t, err)
				require.Len(t, watcher.GetSnapshot().rules, 1)
			} else {
				require.ErrorContains(t, err, "no valid IncidentRules")
			}
		})
	}
}

func TestRulesWatcherRebuildInvalidatesChangedDefinitions(t *testing.T) {
	watcher, _ := testRulesWatcher(t)
	store := watcher.informer.GetStore()
	obj := testConfigMap(t, testRulesYAML("custom", testNodeStrategy))
	require.NoError(t, store.Add(obj))
	watcher.rebuild()
	original := watcher.GetSnapshot()
	require.Len(t, original.rules, 1)

	metadataOnly := obj.DeepCopy()
	metadataOnly.SetAnnotations(map[string]string{"description": "updated"})
	require.NoError(t, store.Update(metadataOnly))
	watcher.rebuild()
	require.Same(t, original, watcher.GetSnapshot())

	changed := testConfigMap(t, testRulesYAML("custom", "  - match:\n      allOf: [namespace]\n    within: 24h\n    from: start\n"))
	require.NoError(t, store.Update(changed))
	watcher.rebuild()
	updated := watcher.GetSnapshot()
	require.NotSame(t, original, updated)
	require.Equal(t, "namespace", string(updated.rules[0].Strategies[0].Keys[0]))
	// Previously published snapshots remain immutable while a processor uses them.
	require.Equal(t, "node", string(original.rules[0].Strategies[0].Keys[0]))

	var log bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&log, nil)))
	defer slog.SetDefault(previousLogger)
	invalid := testConfigMap(t, testRulesYAML("custom", "  - match:\n      anyOf: []\n    within: 24h\n    from: start\n"))
	require.NoError(t, store.Update(invalid))
	watcher.rebuild()
	// Invalid updates are logged and ignored: the last valid snapshot stays.
	require.Same(t, updated, watcher.GetSnapshot())
	require.Contains(t, log.String(), "Cannot load invalid incident rules")
}

func TestRulesWatcherObservesUpdatesAndDeletion(t *testing.T) {
	obj := testConfigMap(t, testRulesYAML("custom", testNodeStrategy))
	watcher, client := testRulesWatcher(t, obj)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, watcher.Start(ctx))
	original := watcher.GetSnapshot()
	changed := testConfigMap(t, testRulesYAML("custom", "  - match:\n      anyOf: [node]\n    within: 24h\n    from: start\n"))
	_, err := client.CoreV1().ConfigMaps(watchNamespace()).Update(ctx, changed, metav1.UpdateOptions{})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return watcher.GetSnapshot() != original && watcher.GetSnapshot().rules[0].Strategies[0].operator == "anyOf"
	}, time.Second, 10*time.Millisecond)
	require.NoError(t, client.CoreV1().ConfigMaps(watchNamespace()).Delete(ctx, ConfigMapName, metav1.DeleteOptions{}))
	// Deleting the ConfigMap keeps the last valid rules; it is not an
	// implicit disable command.
	require.Never(t, func() bool { return len(watcher.GetSnapshot().rules) == 0 }, 500*time.Millisecond, 100*time.Millisecond)
	require.Equal(t, "anyOf", watcher.GetSnapshot().rules[0].Strategies[0].operator)
	// Recreating the ConfigMap allows valid updates to be adopted again.
	recreated := testConfigMap(t, testRulesYAML("custom", testNodeStrategy))
	_, err = client.CoreV1().ConfigMaps(watchNamespace()).Create(ctx, recreated, metav1.CreateOptions{})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return watcher.GetSnapshot().rules[0].Strategies[0].operator == "allOf"
	}, time.Second, 10*time.Millisecond)
}

func TestRulesNamespacePrecedence(t *testing.T) {
	t.Setenv("INCIDENT_RULES_NAMESPACE", "override-ns")
	require.Equal(t, "override-ns", watchNamespace())

	t.Setenv("INCIDENT_RULES_NAMESPACE", "  ")
	// Without the override, the pod namespace file or the default namespace
	// is used; both are nonempty and stable in the test environment.
	require.NotEqual(t, "override-ns", watchNamespace())
	require.NotEmpty(t, watchNamespace())
}

func TestRulesWatcherUpdatesWindowAndPriority(t *testing.T) {
	watcher, _ := testRulesWatcher(t)
	store := watcher.informer.GetStore()
	require.NoError(t, store.Add(testConfigMap(t, "rules:\n- name: z-rule\n  strategies:\n  - match:\n      allOf: [node]\n    within: 1h\n    from: start\n")))
	watcher.rebuild()
	snapshot := watcher.GetSnapshot()
	require.Equal(t, "z-rule", snapshot.rules[0].Name)
	changed := testConfigMap(t, "rules:\n- name: z-rule\n  priority: 20\n  strategies:\n  - match:\n      allOf: [node]\n    within: 48h\n    from: end\n- name: a-rule\n  priority: 10\n  strategies:\n  - match: {}\n    within: 15m\n    from: start\n")
	require.NoError(t, store.Update(changed))
	watcher.rebuild()
	updated := watcher.GetSnapshot()
	require.NotSame(t, snapshot, updated)
	require.Equal(t, "a-rule", updated.rules[0].Name)
	strategy := updated.rules[1].Strategies[0]
	require.Equal(t, 48*time.Hour, strategy.Within)
	require.Equal(t, "end", strategy.From)
	require.EqualValues(t, 20, strategy.Priority)
	require.Equal(t, time.Hour, snapshot.rules[0].Strategies[0].Within)
	require.Equal(t, "start", snapshot.rules[0].Strategies[0].From)
}
