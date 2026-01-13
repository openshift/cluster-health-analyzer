// Package framework provides utilities for integration testing.
package framework

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Cluster provides an interface for interacting with a Kubernetes/OpenShift cluster.
type Cluster struct {
	namespace string
}

// NewCluster creates a new Cluster with the given default namespace.
func NewCluster(namespace string) *Cluster {
	return &Cluster{namespace: namespace}
}

// InNamespace returns a new Cluster with a different default namespace.
func (c *Cluster) InNamespace(namespace string) *Cluster {
	return &Cluster{namespace: namespace}
}

// --- Resource Operations ---

// Apply applies a YAML file using oc apply -f.
func (c *Cluster) Apply(ctx context.Context, yamlPath string) error {
	return c.run(ctx, "apply", "-f", yamlPath)
}

// Delete deletes resources defined in a YAML file.
func (c *Cluster) Delete(ctx context.Context, yamlPath string) error {
	return c.run(ctx, "delete", "-f", yamlPath, "--ignore-not-found")
}

// --- Selectors ---

// GetSelector returns the label selector for a resource (deployment, statefulset, etc).
func (c *Cluster) GetSelector(ctx context.Context, resourceType, name string) (string, error) {
	output, err := c.output(ctx, "get", resourceType, name,
		"-o", "jsonpath={.spec.selector.matchLabels}")
	if err != nil {
		return "", err
	}
	// Convert {"app.kubernetes.io/name":"cluster-health-analyzer"} to app.kubernetes.io/name=cluster-health-analyzer
	output = strings.Trim(output, "{}")
	output = strings.ReplaceAll(output, "\"", "")
	output = strings.ReplaceAll(output, ":", "=")
	return output, nil
}

// --- Deployment Status ---

// IsDeploymentAvailable checks if a deployment is available.
func (c *Cluster) IsDeploymentAvailable(ctx context.Context, name string) (bool, error) {
	output, err := c.output(ctx, "get", "deployment", name,
		"-o", "jsonpath={.status.conditions[?(@.type=='Available')].status}")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(output) == "True", nil
}

// HasUnavailableReplicas checks if a deployment has any unavailable replicas.
func (c *Cluster) HasUnavailableReplicas(ctx context.Context, name string) (bool, error) {
	output, err := c.output(ctx, "get", "deployment", name,
		"-o", "jsonpath={.status.unavailableReplicas}")
	if err != nil {
		return false, err
	}
	trimmed := strings.TrimSpace(output)
	return trimmed != "" && trimmed != "0", nil
}

// --- Pod Status (by label selector) ---

// HasPods checks if any pods exist matching the selector.
func (c *Cluster) HasPods(ctx context.Context, selector string) (bool, error) {
	output, err := c.output(ctx, "get", "pods", "-l", selector,
		"-o", "jsonpath={.items[*].metadata.name}")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(output) != "", nil
}

// ArePodsRunning checks if all pods matching the selector are running.
func (c *Cluster) ArePodsRunning(ctx context.Context, selector string) (bool, error) {
	output, err := c.output(ctx, "get", "pods", "-l", selector,
		"-o", "jsonpath={.items[*].status.phase}")
	if err != nil {
		return false, err
	}
	return allFieldsEqual(output, "Running"), nil
}

// AreContainersReady checks if all containers in pods matching the selector are ready.
func (c *Cluster) AreContainersReady(ctx context.Context, selector string) (bool, error) {
	output, err := c.output(ctx, "get", "pods", "-l", selector,
		"-o", "jsonpath={.items[*].status.containerStatuses[*].ready}")
	if err != nil {
		return false, err
	}
	return allFieldsEqual(output, "true"), nil
}

// allFieldsEqual returns true if output is non-empty and all space-separated values equal expected.
func allFieldsEqual(output, expected string) bool {
	if output == "" {
		return false
	}
	for _, v := range strings.Fields(output) {
		if v != expected {
			return false
		}
	}
	return true
}

// IsPodCrashLooping checks if any pod matching the selector is in CrashLoopBackOff.
func (c *Cluster) IsPodCrashLooping(ctx context.Context, selector string) (bool, error) {
	output, err := c.output(ctx, "get", "pods", "-l", selector,
		"-o", "jsonpath={.items[*].status.containerStatuses[*].state.waiting.reason}")
	if err != nil {
		return false, err
	}
	return strings.Contains(output, "CrashLoopBackOff"), nil
}

// --- Diagnostics ---

// GetPodStatus returns a human-readable status of pods matching the selector.
func (c *Cluster) GetPodStatus(ctx context.Context, selector string) (string, error) {
	return c.output(ctx, "get", "pods", "-l", selector, "-o", "wide")
}

// GetLogs retrieves logs from a resource (e.g., "deployment/my-app").
func (c *Cluster) GetLogs(ctx context.Context, resourceRef string, tailLines int) (string, error) {
	return c.output(ctx, "logs", resourceRef, fmt.Sprintf("--tail=%d", tailLines))
}

// --- Internal helpers ---

func (c *Cluster) run(ctx context.Context, args ...string) error {
	args = c.addNamespace(args)
	cmd := exec.CommandContext(ctx, "oc", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("oc %s failed: %w\nOutput: %s", strings.Join(args, " "), err, output)
	}
	return nil
}

func (c *Cluster) output(ctx context.Context, args ...string) (string, error) {
	args = c.addNamespace(args)
	cmd := exec.CommandContext(ctx, "oc", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("oc %s failed: %w\nStderr: %s", strings.Join(args, " "), err, stderr.String())
	}
	return stdout.String(), nil
}

func (c *Cluster) addNamespace(args []string) []string {
	for _, arg := range args {
		if arg == "-n" || strings.HasPrefix(arg, "--namespace") {
			return args
		}
	}
	return append(args, "-n", c.namespace)
}
