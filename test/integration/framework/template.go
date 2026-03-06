// Package framework provides utilities for integration testing.
package framework

import (
	"bytes"
	"fmt"
	"text/template"
)

// RenderTemplate executes a Go text/template with the given data and returns the result.
// The template string should use standard Go template syntax (e.g. {{ .FieldName }}).
func RenderTemplate(tmplStr string, data any) (string, error) {
	tmpl, err := template.New("").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}
	return buf.String(), nil
}
