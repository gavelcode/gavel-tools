package sarif

import (
	"encoding/json"
	"fmt"
	"os"
)

const filePermission = 0o644

// MarkSuccessful guarantees the SARIF the tool already wrote at path carries an
// executionSuccessful flag on every run, injecting true where the tool emitted
// no invocation. A flag the tool set itself (e.g. PMD's) is left untouched. Use
// for tools that write their own SARIF and completed successfully.
func MarkSuccessful(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read sarif: %w", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("decode sarif: %w", err)
	}
	runs, _ := doc["runs"].([]any)
	for _, entry := range runs {
		run, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if _, exists := run["invocations"]; !exists {
			run["invocations"] = []any{map[string]any{"executionSuccessful": true}}
		}
	}
	return writeJSON(path, doc)
}

// WriteFailed overwrites path with a minimal SARIF reporting that toolName could
// not complete, carrying reason as an error-level notification — for when the
// tool produced no usable output.
func WriteFailed(path, toolName, reason string) error {
	doc := map[string]any{
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
	return writeJSON(path, doc)
}

func writeJSON(path string, doc any) error {
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("encode sarif: %w", err)
	}
	if err := os.WriteFile(path, append(out, '\n'), filePermission); err != nil {
		return fmt.Errorf("write sarif: %w", err)
	}
	return nil
}
