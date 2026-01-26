package framework

import (
	"bytes"
	"fmt"
	"text/template"
)

// Render handles the core logic of parsing and executing a template.
// It accepts any data structure (map or struct).
func Render(name string, templateContent []byte, data any) (string, error) {
	tmpl, err := template.New(name).Parse(string(templateContent))
	if err != nil {
		return "", fmt.Errorf("failed to parse template %s: %w", name, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute template %s: %w", name, err)
	}

	return buf.String(), nil
}
