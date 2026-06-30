package sarif

import (
	"encoding/json"
	"fmt"
	"os"
)

const filePermission = 0o644

// MarkSuccessful guarantees the SARIF the tool already wrote at reportPath carries an
// executionSuccessful flag on every run, injecting true where the tool emitted
// no invocation. A flag the tool set itself (e.g. PMD's) is left untouched. Use
// for tools that write their own SARIF and completed successfully.
func MarkSuccessful(reportPath string) error {
	data, err := os.ReadFile(reportPath)
	if err != nil {
		return fmt.Errorf("read sarif: %w", err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("decode sarif: %w", err)
	}
	runs, _ := document["runs"].([]any)
	for _, entry := range runs {
		run, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if _, exists := run["invocations"]; !exists {
			run["invocations"] = []any{map[string]any{"executionSuccessful": true}}
		}
	}
	return writeJSON(reportPath, document)
}

// WriteFailed overwrites reportPath with a minimal SARIF reporting that toolName could
// not complete, carrying reason as an error-level notification — for when the
// tool produced no usable output.
func WriteFailed(reportPath, toolName, reason string) error {
	document := map[string]any{
		"version": "2.1.0",
		"$schema": "https://json.schemastore.org/sarif-2.1.0.json",
		"runs": []any{map[string]any{
			"tool":    map[string]any{"driver": map[string]any{"name": toolName}},
			"results": []any{},
			"invocations": []any{map[string]any{
				"executionSuccessful": false,
				"toolExecutionNotifications": []any{map[string]any{
					"level":   "error",
					"message": map[string]any{"text": reason},
				}},
			}},
		}},
	}
	return writeJSON(reportPath, document)
}

func writeJSON(reportPath string, document any) error {
	out, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("encode sarif: %w", err)
	}
	if err := os.WriteFile(reportPath, append(out, '\n'), filePermission); err != nil {
		return fmt.Errorf("write sarif: %w", err)
	}
	return nil
}
