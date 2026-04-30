// Package framework provides utilities for integration testing.
package framework

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
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

// Apply applies YAML content to the cluster via stdin.
func (c *Cluster) Apply(ctx context.Context, yamlContent string) error {
	return c.runWithStdin(ctx, yamlContent, "apply", "-f", "-")
}

// Delete removes a specific resource by type and name.
func (c *Cluster) Delete(ctx context.Context, resourceType, name string) error {
	return c.run(ctx, "delete", resourceType, name, "--ignore-not-found")
}

// DeleteByLabel removes all resources of a type matching the label selector.
func (c *Cluster) DeleteByLabel(ctx context.Context, resourceType, labelSelector string) error {
	return c.run(ctx, "delete", resourceType, "-l", labelSelector, "--ignore-not-found")
}

// IsGoneByLabel checks if all resources matching the label selector have been deleted.
// Use this with Eventually to wait for label-based deletion to complete.
func (c *Cluster) IsGoneByLabel(ctx context.Context, resourceType, labelSelector string) (bool, error) {
	output, err := c.output(ctx, "get", resourceType, "-l", labelSelector,
		"-o", "jsonpath={.items[*].metadata.name}")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(output) == "", nil
}

// IsGone checks if a resource no longer exists.
// Use this with Eventually to wait for deletion to complete.
func (c *Cluster) IsGone(ctx context.Context, resourceType, name string) (bool, error) {
	err := c.run(ctx, "get", resourceType, name)
	if err != nil {
		// "not found" or "NotFound" means it's gone (success!)
		errStr := err.Error()
		if strings.Contains(errStr, "not found") || strings.Contains(errStr, "NotFound") {
			return true, nil
		}
		return false, err
	}
	return false, nil
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

// AllReplicasUpdated checks whether all replicas have been updated to the latest pod template.
func (c *Cluster) AllReplicasUpdated(ctx context.Context, name string) (bool, error) {
	output, err := c.output(ctx, "get", "deployment", name,
		"-o", "jsonpath={.spec.replicas} {.status.updatedReplicas}")
	if err != nil {
		return false, err
	}
	parts := strings.Fields(output)
	if len(parts) != 2 {
		return false, nil
	}
	desired, err := strconv.Atoi(parts[0])
	if err != nil {
		return false, fmt.Errorf("failed to parse spec.replicas: %w", err)
	}
	updated, err := strconv.Atoi(parts[1])
	if err != nil {
		return false, fmt.Errorf("failed to parse status.updatedReplicas: %w", err)
	}
	return updated == desired, nil
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

// --- Deployment Management ---

// GetDeploymentArgs returns the args of the container at containerIndex in the deployment.
func (c *Cluster) GetDeploymentArgs(ctx context.Context, deploymentName string, containerIndex int) ([]string, error) {
	output, err := c.output(ctx, "get", "deployment", deploymentName,
		"-o", fmt.Sprintf("jsonpath={.spec.template.spec.containers[%d].args}", containerIndex))
	if err != nil {
		return nil, err
	}
	var args []string
	if err := json.Unmarshal([]byte(output), &args); err != nil {
		return nil, fmt.Errorf("failed to parse deployment args: %w", err)
	}
	return args, nil
}

// SetDeploymentArgs replaces the args of the container at containerIndex in the deployment.
func (c *Cluster) SetDeploymentArgs(ctx context.Context, deploymentName string, containerIndex int, args []string) error {
	if args == nil {
		args = []string{}
	}
	patch, err := json.Marshal(args)
	if err != nil {
		return err
	}
	patchJSON := fmt.Sprintf(`[{"op":"add","path":"/spec/template/spec/containers/%d/args","value":%s}]`,
		containerIndex, patch)
	return c.run(ctx, "patch", "deployment", deploymentName, "--type=json", "-p", patchJSON)
}

// WaitForRollout waits for the deployment to finish rolling out.
// It fails fast if any pod enters CrashLoopBackOff rather than waiting for the full timeout.
func (c *Cluster) WaitForRollout(ctx context.Context, deploymentName string) error {
	const (
		timeout      = 120 * time.Second
		pollInterval = 5 * time.Second
	)

	selector, err := c.GetSelector(ctx, "deployment", deploymentName)
	if err != nil {
		return err
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		crashing, err := c.IsPodCrashLooping(ctx, selector)
		if err != nil {
			return err
		}
		if crashing {
			return fmt.Errorf("rollout of %s failed: pod entered CrashLoopBackOff", deploymentName)
		}

		available, err := c.IsDeploymentAvailable(ctx, deploymentName)
		if err != nil {
			return err
		}
		hasUnavailable, err := c.HasUnavailableReplicas(ctx, deploymentName)
		if err != nil {
			return err
		}
		allUpdated, err := c.AllReplicasUpdated(ctx, deploymentName)
		if err != nil {
			return err
		}
		if available && !hasUnavailable && allUpdated {
			return nil
		}

		time.Sleep(pollInterval)
	}

	return fmt.Errorf("rollout of %s did not complete within %s", deploymentName, timeout)
}

// --- Port Forwarding ---

var portForwardRe = regexp.MustCompile(`Forwarding from 127\.0\.0\.1:(\d+)`)

// PortForward starts a port-forward to the given resource (e.g. "svc/my-service")
// and remote port. It returns the local port assigned by the OS and a cancel
// function that stops the port-forward. The caller must call cancel when done.
func (c *Cluster) PortForward(resourceRef string, remotePort int) (localPort int, cancel func(), err error) {
	const startupTimeout = 30 * time.Second

	args := c.addNamespace([]string{"port-forward", resourceRef, fmt.Sprintf("0:%d", remotePort)})
	startupCtx, startupCancel := context.WithTimeout(context.Background(), startupTimeout)
	defer startupCancel()

	cmd := exec.Command("oc", args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, nil, err
	}
	if err := cmd.Start(); err != nil {
		return 0, nil, err
	}

	cancel = func() { _ = cmd.Process.Kill() }

	type scanResult struct {
		port int
		err  error
	}
	ch := make(chan scanResult, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if matches := portForwardRe.FindStringSubmatch(scanner.Text()); matches != nil {
				port, parseErr := strconv.Atoi(matches[1])
				if parseErr != nil {
					ch <- scanResult{err: fmt.Errorf("failed to parse port from port-forward output: %w", parseErr)}
					return
				}
				ch <- scanResult{port: port}
				return
			}
		}
		ch <- scanResult{err: fmt.Errorf("port-forward to %s:%d did not output expected forwarding line", resourceRef, remotePort)}
	}()

	select {
	case res := <-ch:
		if res.err != nil {
			cancel()
			return 0, nil, res.err
		}
		return res.port, cancel, nil
	case <-startupCtx.Done():
		cancel()
		return 0, nil, fmt.Errorf("port-forward to %s:%d timed out after %s waiting for forwarding line", resourceRef, remotePort, startupTimeout)
	}
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


func (c *Cluster) runWithStdin(ctx context.Context, stdin string, args ...string) error {
	args = c.addNamespace(args)
	cmd := exec.CommandContext(ctx, "oc", args...)
	cmd.Stdin = strings.NewReader(stdin)
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
