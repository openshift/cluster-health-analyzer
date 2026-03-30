package simulate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// createTSDBBlocks uses promtool to create TSDB blocks from an OpenMetrics file.
func createTSDBBlocks(ctx context.Context, openmetricsFile, dataDir string) error {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data dir: %w", err)
	}

	cmd := exec.CommandContext(ctx, "promtool", "tsdb", "create-blocks-from", "openmetrics",
		openmetricsFile, dataDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("promtool failed: %w\nOutput: %s", err, string(output))
	}
	return nil
}

// copyBlocksToPrometheusWithFallback copies TSDB blocks to one of the Prometheus pods.
// It tries each pod in order and returns the name of the pod that succeeded.
// If a copy partially succeeds then fails, the already-copied blocks are removed
// from that pod before trying the next one, to avoid duplicate data across pods.
// If all pods fail, it returns an error with details from all attempts.
func copyBlocksToPrometheusWithFallback(ctx context.Context, dataDir, namespace string, pods []string) (string, error) {
	var lastErr error

	for _, pod := range pods {
		slog.Info("Attempting to copy blocks to Prometheus pod", "pod", pod)
		copiedBlocks, err := copyBlocksToPrometheus(ctx, dataDir, namespace, pod)
		if err == nil {
			slog.Info("Successfully copied blocks to Prometheus pod", "pod", pod)
			return pod, nil
		}
		slog.Warn("Failed to copy blocks to pod, trying next", "pod", pod, "error", err)
		if len(copiedBlocks) > 0 {
			cleanupBlocksFromPod(ctx, namespace, pod, copiedBlocks)
		}
		lastErr = err
	}

	return "", fmt.Errorf("all Prometheus pods failed, last error: %w", lastErr)
}

// copyBlocksToPrometheus copies TSDB blocks to a specific Prometheus pod.
// Returns the names of blocks that were successfully copied (useful for cleanup on partial failure).
func copyBlocksToPrometheus(ctx context.Context, dataDir, namespace, pod string) (copiedBlocks []string, retErr error) {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read data dir: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		blockPath := filepath.Join(dataDir, entry.Name())
		destPath := fmt.Sprintf("%s/%s:/prometheus", namespace, pod)

		cmd := exec.CommandContext(ctx, "oc", "cp", blockPath, destPath, "-c", "prometheus")
		output, err := cmd.CombinedOutput()
		if err != nil {
			return copiedBlocks, fmt.Errorf("oc cp failed for block %s: %w\nOutput: %s",
				entry.Name(), err, string(output))
		}
		copiedBlocks = append(copiedBlocks, entry.Name())
	}

	return copiedBlocks, nil
}

// cleanupBlocksFromPod removes specific TSDB blocks from a Prometheus pod.
// This is best-effort with a short timeout: if the pod is down, we bail quickly
// rather than blocking the fallback to the next pod. Failures are logged but
// do not propagate, since the caller is already in error recovery.
func cleanupBlocksFromPod(ctx context.Context, namespace, pod string, blocks []string) {
	cleanupCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	slog.Info("Cleaning up partially-copied blocks from failed pod", "pod", pod, "blocks", blocks)
	for _, block := range blocks {
		rmCmd := exec.CommandContext(cleanupCtx, "oc", "exec", "-n", namespace, pod, "-c", "prometheus",
			"--", "rm", "-rf", filepath.Join("/prometheus", block))
		if output, err := rmCmd.CombinedOutput(); err != nil {
			slog.Warn("Failed to clean up block from pod", "pod", pod, "block", block,
				"error", err, "output", string(output))
			if cleanupCtx.Err() != nil {
				slog.Warn("Cleanup timed out, skipping remaining blocks", "pod", pod)
				return
			}
		}
	}
}

// wipePrometheusData deletes all Prometheus data and restarts the pods.
// This is a "nuke" approach that guarantees a clean slate by:
// 1. Deleting all contents of /prometheus/ on each pod
// 2. Deleting the pods to force a fresh restart
// 3. Waiting for pods to be ready again
// WARNING: This destroys ALL metrics data, not just test data.
// Only use on dedicated test clusters.
func wipePrometheusData(ctx context.Context, namespace string, pods []string) error {
	slog.Info("Wiping all Prometheus data (nuke mode)")

	// Step 1: Delete all data from each pod
	for _, pod := range pods {
		slog.Info("Wiping data from pod", "pod", pod)
		wipeCmd := exec.CommandContext(ctx, "oc", "exec", "-n", namespace, pod, "-c", "prometheus", "--",
			"sh", "-c", "rm -rf /prometheus/*")
		if output, err := wipeCmd.CombinedOutput(); err != nil {
			slog.Warn("Failed to wipe data from pod", "pod", pod, "error", err, "output", string(output))
			// Continue - pod might not exist or be accessible
		} else {
			slog.Info("Wiped data from pod", "pod", pod)
		}
	}

	// Step 2: Delete pods individually to force restart with clean state.
	// Pods that don't exist are skipped so the function works when only a
	// subset of the fallback list is present on the cluster.
	var deletedPods []string
	for _, pod := range pods {
		slog.Info("Deleting Prometheus pod", "pod", pod)
		deleteCmd := exec.CommandContext(ctx, "oc", "delete", "pod", pod, "-n", namespace, "--wait=false")
		output, err := deleteCmd.CombinedOutput()
		if err != nil {
			outStr := string(output)
			if strings.Contains(outStr, "NotFound") || strings.Contains(outStr, "not found") {
				slog.Info("Pod not found, skipping", "pod", pod)
				continue
			}
			return fmt.Errorf("failed to delete pod %s: %w\nOutput: %s", pod, err, outStr)
		}
		deletedPods = append(deletedPods, pod)
	}

	// Step 3: Wait for deleted pods to be ready again (parallel)
	slog.Info("Waiting for Prometheus pods to be ready", "pods", deletedPods)
	var wg sync.WaitGroup
	errCh := make(chan error, len(deletedPods))
	for _, pod := range deletedPods {
		wg.Add(1)
		go func(pod string) {
			defer wg.Done()
			waitCmd := exec.CommandContext(ctx, "oc", "wait", "pod", pod, "-n", namespace,
				"--for=condition=Ready", "--timeout=5m")
			if output, err := waitCmd.CombinedOutput(); err != nil {
				errCh <- fmt.Errorf("timeout waiting for pod %s to be ready: %w\nOutput: %s", pod, err, string(output))
				return
			}
			slog.Info("Pod is ready", "pod", pod)
		}(pod)
	}
	wg.Wait()
	close(errCh)

	var waitErrs []error
	for err := range errCh {
		waitErrs = append(waitErrs, err)
	}
	if len(waitErrs) > 0 {
		return fmt.Errorf("pods failed to become ready: %w", errors.Join(waitErrs...))
	}

	slog.Info("Prometheus data wiped and pods ready")
	return nil
}

