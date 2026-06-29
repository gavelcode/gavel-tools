package sarif

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarkSuccessful_InjectsWhenAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.sarif")
	require.NoError(t, os.WriteFile(path, []byte(`{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"ruff"}},"results":[]}]}`), 0o644))

	require.NoError(t, MarkSuccessful(path))

	run := readRun(t, path)
	inv := run["invocations"].([]any)[0].(map[string]any)
	assert.Equal(t, true, inv["executionSuccessful"])
}

func TestMarkSuccessful_KeepsExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.sarif")
	require.NoError(t, os.WriteFile(path, []byte(`{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"pmd"}},"results":[],"invocations":[{"executionSuccessful":false}]}]}`), 0o644))

	require.NoError(t, MarkSuccessful(path))

	run := readRun(t, path)
	inv := run["invocations"].([]any)[0].(map[string]any)
	assert.Equal(t, false, inv["executionSuccessful"])
}

func TestWriteFailed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.sarif")

	require.NoError(t, WriteFailed(path, "ruff", "invalid config"))

	run := readRun(t, path)
	inv := run["invocations"].([]any)[0].(map[string]any)
	assert.Equal(t, false, inv["executionSuccessful"])
	note := inv["toolExecutionNotifications"].([]any)[0].(map[string]any)
	assert.Equal(t, "error", note["level"])
	assert.Contains(t, note["message"].(map[string]any)["text"], "invalid config")
}

func readRun(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(data, &doc))
	return doc["runs"].([]any)[0].(map[string]any)
}
