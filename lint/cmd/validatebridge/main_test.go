package validatebridge

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validSARIF = `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"todo"}},"results":[
 {"ruleId":"todo/found","level":"warning","message":{"text":"TODO found"},
  "locations":[{"physicalLocation":{"artifactLocation":{"uri":"pkg/a.txt"},"region":{"startLine":3}}}]}]}]}`

const invalidSARIF = `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":""}},"results":[]}]}`

func write(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestRun_ValidFileExitsZeroAndPrintsNothing(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{write(t, "ok.sarif", validSARIF)}, &stdout, &stderr)

	assert.Equal(t, 0, code)
	assert.Empty(t, stdout.String())
	assert.Empty(t, stderr.String())
}

func TestRun_InvalidFileListsViolationsAndExitsOne(t *testing.T) {
	path := write(t, "bad.sarif", invalidSARIF)
	var stdout, stderr bytes.Buffer

	code := run([]string{path}, &stdout, &stderr)

	assert.Equal(t, 1, code)
	assert.Equal(t, path+": runs[0]: tool.driver.name must be non-empty\n", stdout.String())
}

func TestRun_ReportsEveryInvalidFileEvenWhenAnotherIsValid(t *testing.T) {
	good := write(t, "ok.sarif", validSARIF)
	bad := write(t, "bad.sarif", invalidSARIF)
	var stdout, stderr bytes.Buffer

	code := run([]string{bad, good, bad}, &stdout, &stderr)

	assert.Equal(t, 1, code)
	assert.Equal(t, 2, bytes.Count(stdout.Bytes(), []byte("tool.driver.name")))
	assert.NotContains(t, stdout.String(), good)
}

func TestRun_MissingFileIsIOError(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{filepath.Join(t.TempDir(), "absent.sarif")}, &stdout, &stderr)

	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "absent.sarif")
	assert.Empty(t, stdout.String())
}

func TestRun_NoArgumentsIsUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run(nil, &stdout, &stderr)

	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "usage: gavel-lint validate-bridge FILE.sarif...")
}

func TestRun_RelativePathsResolveAgainstBazelWorkingDirectory(t *testing.T) {
	path := write(t, "bad.sarif", invalidSARIF)
	t.Setenv("BUILD_WORKING_DIRECTORY", filepath.Dir(path))
	var stdout, stderr bytes.Buffer

	code := run([]string{"bad.sarif"}, &stdout, &stderr)

	assert.Equal(t, 1, code)
	assert.Contains(t, stdout.String(), "bad.sarif: runs[0]")
}

func TestMain_DelegatesToProcessStreams(t *testing.T) {
	assert.Equal(t, 0, Main([]string{write(t, "ok.sarif", validSARIF)}))
	assert.Equal(t, 2, Main(nil))
}
