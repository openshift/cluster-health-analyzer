package componentshealth

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/inecas/kube-health/pkg/eval"
	"github.com/inecas/kube-health/pkg/khealth"
	"github.com/inecas/kube-health/pkg/status"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

type HealthChecker interface {
	EvaluateObjects(ctx context.Context, objects []K8sObject) ([]ObjectStatus, error)
}

// kubeHealthChecker is wrapper type
// around kube-health
type kubeHealthChecker struct {
	evaluator *eval.Evaluator
}

// NewKubeHealthChecker creates a new instance of the
// kubeHealthChecker.
func NewKubeHealthChecker() (HealthChecker, error) {
	evaluator, err := khealth.NewHealthEvaluator(nil)
	if err != nil {
		return nil, err
	}
	khChecker := &kubeHealthChecker{
		evaluator: evaluator,
	}

	return khChecker, nil
}

// EvaluateObjects evaluates health of the objects with the kube-health. Returns
// slice of object statuses.
func (k *kubeHealthChecker) EvaluateObjects(ctx context.Context, objects []K8sObject) ([]ObjectStatus, error) {
	statuses := make(map[types.UID]ObjectStatus)
	for _, o := range objects {
		gr := schema.GroupResource{Group: o.Group, Resource: o.Resource}

		if len(o.ObjectsSelectors) > 0 {
			labelExpressions := buildLabelExpressions(o.ObjectsSelectors)
			for _, le := range labelExpressions {
				objStatuses, err := k.evaluator.EvalResourceWithSelector(ctx, gr, o.Namespace, le)
				if err != nil {
					return nil, err
				}
				aggregateObjectStatuses(statuses, objStatuses, gr)
			}
		} else {
			objStatuses, err := k.evaluator.EvalResource(ctx, gr, o.Namespace, o.Name)
			if err != nil {
				return nil, err
			}
			aggregateObjectStatuses(statuses, objStatuses, gr)
		}
	}
	return slices.Collect(maps.Values(statuses)), nil
}

func aggregateObjectStatuses(m map[types.UID]ObjectStatus, objectStatuses []status.ObjectStatus, gr schema.GroupResource) {
	for _, os := range objectStatuses {
		_, exist := m[os.Object.UID]
		if exist {
			continue
		}

		objStatus := ObjectStatus{
			Name:         os.Object.Name,
			Namespace:    os.Object.Namespace,
			Resource:     gr.Resource,
			HealthStatus: ParseKubeHealthStatus(os.Status().Result),
			Progressing:  os.Status().Progressing,
		}
		m[os.Object.UID] = objStatus
	}
}

// buildLabelExpressions iterates over the provided
// selectors and creates a label expression for each set (matchLabels)
// of label pairs. Example:
//
//	key1: [v1] translates to "key1=value1"
//	key1: [v1, v2] and key2 translates to "key1 in (v1,v2),key2"
func buildLabelExpressions(selectors []Selector) []string {
	labelExpressions := make([]string, 0, len(selectors))

	for _, s := range selectors {
		labelNames := slices.Collect(maps.Keys(s.MatchLabels))
		sort.Strings(labelNames)
		parts := make([]string, 0, len(labelNames))

		for _, lName := range labelNames {
			lValues := s.MatchLabels[lName]

			switch {
			case len(lValues) == 0:
				parts = append(parts, lName)
			case len(lValues) == 1:
				parts = append(parts, fmt.Sprintf("%s=%s", lName, lValues[0]))
			case len(lValues) > 1:
				var sb strings.Builder
				sb.WriteString(lName)
				sb.WriteString(" in (")
				for i, v := range lValues {
					if i > 0 {
						sb.WriteString(",")
					}
					sb.WriteString(v)
				}
				sb.WriteString(")")
				parts = append(parts, sb.String())
			}
		}
		expression := strings.Join(parts, ",")
		labelExpressions = append(labelExpressions, expression)
	}

	return labelExpressions
}
