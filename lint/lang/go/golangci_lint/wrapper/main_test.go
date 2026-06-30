package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetFlags(t *testing.T) {
	t.Helper()
	orig := flag.CommandLine
	t.Cleanup(func() { flag.CommandLine = orig })
	flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
}

func setArgs(t *testing.T, args []string) {
	t.Helper()
	orig := os.Args
	t.Cleanup(func() { os.Args = orig })
	os.Args = args
}

const fakeGolangciEmit = `#!/bin/sh
for a in "$@"; do case "$a" in --output.sarif.path=*) o="${a#--output.sarif.path=}";; esac; done
[ -n "$o" ] && printf '{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"golangci-lint"}},"results":[]}]}' > "$o"
`

func writeFakeScript(t *testing.T, name string, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, name)
	script := fakeGolangciEmit + fmt.Sprintf("exit %d\n", exitCode)
	require.NoError(t, os.WriteFile(bin, []byte(script), 0o755))
	return bin
}

func TestExecute_MissingPackage(t *testing.T) {
	resetFlags(t)
	setArgs(t, []string{"test", "--out", "/tmp/out.sarif"})

	assert.Equal(t, 2, execute())
}

func TestExecute_MissingOut(t *testing.T) {
	resetFlags(t)
	setArgs(t, []string{"test", "--package", "./internal/order"})

	assert.Equal(t, 2, execute())
}

func TestExecute_RunError(t *testing.T) {
	resetFlags(t)
	t.Setenv("PATH", t.TempDir())
	dir := t.TempDir()
	setArgs(t, []string{"test", "--package", "./pkg", "--out", filepath.Join(dir, "out.sarif")})

	assert.Equal(t, 1, execute())
}

func TestExecute_Success(t *testing.T) {
	lint := writeFakeScript(t, "golangci-lint", 0)
	resetFlags(t)
	dir := t.TempDir()
	setArgs(t, []string{"test", "--golangci-lint", lint, "--package", "./pkg", "--out", filepath.Join(dir, "out.sarif")})

	assert.Equal(t, 0, execute())
}

func TestRun_MkdirAllError(t *testing.T) {
	lint := writeFakeScript(t, "golangci-lint", 0)

	err := run(lint, "", "", "", "./pkg", "/dev/null/impossible/out.sarif", false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "create output dir")
}

func TestRun_LintNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	dir := t.TempDir()

	err := run("", "", "", "", "./pkg", filepath.Join(dir, "out.sarif"), false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRun_LintFoundInPath(t *testing.T) {
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "golangci-lint")
	require.NoError(t, os.WriteFile(bin, []byte(fakeGolangciEmit+"exit 0\n"), 0o755))
	t.Setenv("PATH", tmp)
	dir := t.TempDir()

	err := run("", "", "", "", "./pkg", filepath.Join(dir, "out.sarif"), false)

	require.NoError(t, err)
}

func TestRun_CommandErrorWritesFailedSarif(t *testing.T) {
	lint := writeFakeScript(t, "golangci-lint", 1)
	dir := t.TempDir()
	out := filepath.Join(dir, "out.sarif")

	err := run(lint, "", "", "", "./pkg", out, false)

	require.NoError(t, err)
	data, readErr := os.ReadFile(out)
	require.NoError(t, readErr)
	assert.Contains(t, string(data), `"executionSuccessful": false`)
}

func TestRun_WithConfigFile(t *testing.T) {
	lint := writeFakeScript(t, "golangci-lint", 0)
	dir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".golangci.yml"), []byte("linters:\n"), 0o644))

	err = run(lint, "", "", "", "./pkg", filepath.Join(dir, "out", "result.sarif"), false)

	require.NoError(t, err)
}

func TestBuildArgs_SkipTestsAddsFlag(t *testing.T) {
	args := buildLintArgs("./internal/order", "/tmp/out.sarif", true, "")

	assert.Contains(t, args, "--tests=false")
}

func TestBuildArgs_WithoutSkipTestsOmitsFlag(t *testing.T) {
	args := buildLintArgs("./internal/order", "/tmp/out.sarif", false, "")

	for _, arg := range args {
		assert.NotContains(t, arg, "--tests", "must not include --tests flag when skipTests is false")
	}
}

func TestBuildArgs_AlwaysHasRunAndPackage(t *testing.T) {
	args := buildLintArgs("./internal/order", "/tmp/out.sarif", false, "")

	assert.Equal(t, "run", args[0])
	assert.Equal(t, "./internal/order", args[1])
}

func TestBuildArgs_WithConfigFile(t *testing.T) {
	args := buildLintArgs("./pkg", "/tmp/out.sarif", false, ".golangci.yml")

	assert.Contains(t, args, "--config=.golangci.yml")
}

func TestBuildArgs_SARIFOutput(t *testing.T) {
	args := buildLintArgs("./pkg", "/tmp/out.sarif", false, "")

	found := false
	for _, arg := range args {
		if strings.HasPrefix(arg, "--output.sarif.path=") {
			found = true
			assert.Equal(t, "--output.sarif.path=/tmp/out.sarif", arg)
		}
	}
	assert.True(t, found, "SARIF output path must be present")
}

func TestDriverEnv_PointsGolangciAtStaticDriver(t *testing.T) {
	env := envToMap(driverEnv(t.TempDir(), "/exec/driver", "/exec/manifest", ""))

	assert.Equal(t, "/exec/driver", env["GOPACKAGESDRIVER"])
	assert.Equal(t, "/exec/manifest", env["GAVEL_PKG_JSON_MANIFEST"])
}

func TestDriverEnv_CutsNetwork(t *testing.T) {
	env := envToMap(driverEnv(t.TempDir(), "", "", ""))

	assert.Equal(t, "off", env["GOPROXY"])
	assert.Equal(t, "off", env["GOSUMDB"])
	assert.Equal(t, "local", env["GOTOOLCHAIN"])
}

func TestDriverEnv_DerivesGOROOTFromSDK(t *testing.T) {
	env := envToMap(driverEnv(t.TempDir(), "", "", "/sdk/go/bin/go"))

	assert.Equal(t, "/sdk/go", env["GOROOT"])
}

func TestDriverEnv_OmitsGOROOTWhenNoGoBinary(t *testing.T) {
	env := envToMap(driverEnv(t.TempDir(), "", "", ""))

	_, ok := env["GOROOT"]
	assert.False(t, ok, "GOROOT must be absent when no --go is provided")
}

func TestDriverEnv_IsolatedCaches(t *testing.T) {
	cache := t.TempDir()
	env := envToMap(driverEnv(cache, "", "", ""))

	assert.Equal(t, filepath.Join(cache, "build"), env["GOCACHE"])
	assert.Equal(t, filepath.Join(cache, "lint"), env["GOLANGCI_LINT_CACHE"])
}

func TestGoRoot_StripsBinGo(t *testing.T) {
	assert.Equal(t, "/sdk/root", goRoot("/sdk/root/bin/go"))
}

func TestBinPath_PrependsSDKBin(t *testing.T) {
	got := binPath("/sdk/root/bin/go")

	assert.True(t, strings.HasPrefix(got, "/sdk/root/bin:"), "SDK bin must come first so `go` resolves")
	assert.True(t, strings.HasSuffix(got, "/usr/bin:/bin"))
}

func TestBinPath_SystemOnlyWhenNoGo(t *testing.T) {
	assert.Equal(t, "/usr/bin:/bin", binPath(""))
}

func TestAbsOrSelf_MakesRelativeAbsolute(t *testing.T) {
	got := absOrSelf("rel/path")

	assert.True(t, filepath.IsAbs(got), "relative path should become absolute")
	assert.True(t, strings.HasSuffix(got, filepath.Join("rel", "path")))
}

func envToMap(env []string) map[string]string {
	result := make(map[string]string, len(env))
	for _, item := range env {
		if key, val, ok := strings.Cut(item, "="); ok {
			result[key] = val
		}
	}
	return result
}
