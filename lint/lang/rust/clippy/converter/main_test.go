package converter

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain_MissingFlagsIsUsageError(t *testing.T) {
	assert.Equal(t, 2, Main([]string{"--in", "x"}))
	assert.Equal(t, 2, Main([]string{"--out", "x"}))
}

func TestMain_MissingInputFails(t *testing.T) {
	dir := t.TempDir()

	code := Main([]string{"--in", filepath.Join(dir, "nope.json"), "--out", filepath.Join(dir, "o.sarif")})

	assert.Equal(t, 1, code)
}

func TestMain_UnwritableOutputFails(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "d.json")
	require.NoError(t, os.WriteFile(in, []byte(""), 0o644))

	code := Main([]string{"--in", in, "--out", "/dev/null/impossible/o.sarif"})

	assert.Equal(t, 1, code)
}

func TestMain_ConvertsDiagnosticsToSarif(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "d.json")
	out := filepath.Join(dir, "o.sarif")
	require.NoError(t, os.WriteFile(in, []byte(""), 0o644))

	code := Main([]string{"--in", in, "--out", out})

	assert.Equal(t, 0, code)
	data, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"version": "2.1.0"`)
}
