package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withStub swaps the registry for a single recording tool so dispatch is tested
// without running a real wrapper.
func withStub(t *testing.T, name string, code int) *[]string {
	t.Helper()
	orig := tools
	t.Cleanup(func() { tools = orig })
	got := new([]string)
	tools = map[string]func([]string) int{
		name: func(args []string) int {
			*got = args
			return code
		},
	}
	return got
}

func TestDispatch_RoutesToolWithRemainingArgs(t *testing.T) {
	got := withStub(t, "stub", 7)

	code := dispatch([]string{"gavel-lint", "stub", "--out", "x", "file"}, &bytes.Buffer{})

	assert.Equal(t, 7, code)
	assert.Equal(t, []string{"--out", "x", "file"}, *got)
}

func TestDispatch_NoToolIsUsageError(t *testing.T) {
	withStub(t, "stub", 0)
	var stderr bytes.Buffer

	code := dispatch([]string{"gavel-lint"}, &stderr)

	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "usage: gavel-lint <tool>")
	assert.Contains(t, stderr.String(), "stub")
}

func TestDispatch_UnknownToolIsUsageError(t *testing.T) {
	withStub(t, "stub", 0)
	var stderr bytes.Buffer

	code := dispatch([]string{"gavel-lint", "nope"}, &stderr)

	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), `unknown tool "nope"`)
}

func TestDispatch_LauncherNameRunsPackagesDriver(t *testing.T) {
	got := withStub(t, "packagesdriver", 0)

	code := dispatch([]string{"/tmp/x/" + packagesDriverLauncher, "-pattern", "./..."}, &bytes.Buffer{})

	assert.Equal(t, 0, code)
	assert.Equal(t, []string{"-pattern", "./..."}, *got)
}

func TestTools_AreAllRegistered(t *testing.T) {
	want := []string{
		"bandit", "clippy-convert", "cpd", "eslint", "error-prone", "go-archtest",
		"golangci", "java-archtest", "packagesdriver", "pmd", "pycompile",
		"python-archtest", "ruff", "rust-archtest", "spotbugs", "typescript-archtest",
	}
	got := make([]string, 0, len(tools))
	for name, fn := range tools {
		require.NotNil(t, fn, name)
		got = append(got, name)
	}
	assert.ElementsMatch(t, want, got)
}
