package simulate

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CreateTSDBBlocks uses promtool to create TSDB blocks from an OpenMetrics file.
func CreateTSDBBlocks(openmetricsFile, dataDir string) error {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data dir: %w", err)
	}

	cmd := exec.Command("promtool", "tsdb", "create-blocks-from", "openmetrics",
		openmetricsFile, dataDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("promtool failed: %w\nOutput: %s", err, string(output))
	}
	return nil
}

// CopyBlocksToPrometheusWithFallback copies TSDB blocks to one of the Prometheus pods.
// It tries each pod in order and returns the name of the pod that succeeded.
// If all pods fail, it returns an error with details from all attempts.
func CopyBlocksToPrometheusWithFallback(dataDir, namespace string, pods []string) (string, error) {
	var lastErr error

	for _, pod := range pods {
		slog.Info("Attempting to copy blocks to Prometheus pod", "pod", pod)
		err := CopyBlocksToPrometheus(dataDir, namespace, pod)
		if err == nil {
			slog.Info("Successfully copied blocks to Prometheus pod", "pod", pod)
			return pod, nil
		}
		slog.Warn("Failed to copy blocks to pod, trying next", "pod", pod, "error", err)
		lastErr = err
	}

	return "", fmt.Errorf("all Prometheus pods failed, last error: %w", lastErr)
}

// CopyBlocksToPrometheus copies TSDB blocks to a specific Prometheus pod.
func CopyBlocksToPrometheus(dataDir, namespace, pod string) error {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return fmt.Errorf("failed to read data dir: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		blockPath := filepath.Join(dataDir, entry.Name())
		destPath := fmt.Sprintf("%s/%s:/prometheus", namespace, pod)

		cmd := exec.Command("oc", "cp", blockPath, destPath, "-c", "prometheus")
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("oc cp failed for block %s: %w\nOutput: %s",
				entry.Name(), err, string(output))
		}
	}

	return nil
}

// WipePrometheusData deletes all Prometheus data and restarts the pods.
// This is a "nuke" approach that guarantees a clean slate by:
// 1. Deleting all contents of /prometheus/ on each pod
// 2. Deleting the pods to force a fresh restart
// 3. Waiting for pods to be ready again
// WARNING: This destroys ALL metrics data, not just test data.
// Only use on dedicated test clusters.
func WipePrometheusData(namespace string, pods []string) error {
	slog.Info("Wiping all Prometheus data (nuke mode)")

	// Step 1: Delete all data from each pod
	for _, pod := range pods {
		slog.Info("Wiping data from pod", "pod", pod)
		wipeCmd := exec.Command("oc", "exec", "-n", namespace, pod, "-c", "prometheus", "--",
			"sh", "-c", "rm -rf /prometheus/*")
		if output, err := wipeCmd.CombinedOutput(); err != nil {
			slog.Warn("Failed to wipe data from pod", "pod", pod, "error", err, "output", string(output))
			// Continue - pod might not exist or be accessible
		} else {
			slog.Info("Wiped data from pod", "pod", pod)
		}
	}

	// Step 2: Delete all pods to force restart with clean state
	podNames := strings.Join(pods, " ")
	slog.Info("Deleting Prometheus pods to force restart", "pods", podNames)
	deleteCmd := exec.Command("oc", "delete", "pod", "-n", namespace, "--wait=false")
	deleteCmd.Args = append(deleteCmd.Args, pods...)
	if output, err := deleteCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to delete pods: %w\nOutput: %s", err, string(output))
	}

	// Step 3: Wait for pods to be ready again
	slog.Info("Waiting for Prometheus pods to be ready")
	for _, pod := range pods {
		waitCmd := exec.Command("oc", "wait", "pod", pod, "-n", namespace,
			"--for=condition=Ready", "--timeout=5m")
		if output, err := waitCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("timeout waiting for pod %s to be ready: %w\nOutput: %s", pod, err, string(output))
		}
		slog.Info("Pod is ready", "pod", pod)
	}

	slog.Info("Prometheus data wiped and pods ready")
	return nil
}

