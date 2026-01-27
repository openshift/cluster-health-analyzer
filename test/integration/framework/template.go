// Package framework provides utilities for integration testing.
package framework

import (
	"fmt"
	"os"
	"strings"
)

// LoadTemplate reads a file and returns its content as a string.
func LoadTemplate(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read template %s: %w", path, err)
	}
	return string(data), nil
}

// RenderTemplate replaces all placeholders in template with their values.
// Placeholders should be in the format {{KEY}}.
func RenderTemplate(template string, replacements map[string]string) string {
	result := template
	for placeholder, value := range replacements {
		result = strings.ReplaceAll(result, placeholder, value)
	}
	return result
}

// LoadAndRender loads a template file and renders it with the given replacements.
func LoadAndRender(path string, replacements map[string]string) (string, error) {
	template, err := LoadTemplate(path)
	if err != nil {
		return "", err
	}
	return RenderTemplate(template, replacements), nil
}
