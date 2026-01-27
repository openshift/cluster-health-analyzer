package simulate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// runSimulate executes the simulate command to generate an OpenMetrics file.
// If alertsOnly is true, only ALERTS metrics are generated (for integration testing).
func runSimulate(projectRoot, scenarioFile, outputFile string, alertsOnly bool) error {
	args := []string{"run", "./main.go", "simulate",
		"--scenario", scenarioFile,
		"--output", outputFile}

	if alertsOnly {
		args = append(args, "--alerts-only")
	}

	cmd := exec.Command("go", args...)
	cmd.Dir = projectRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("simulate failed: %w\nOutput: %s", err, string(output))
	}
	return nil
}

// findProjectRoot walks up from cwd to find go.mod.
func findProjectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find project root (no go.mod found)")
		}
		dir = parent
	}
}
