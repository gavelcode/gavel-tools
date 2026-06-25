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

func writeFakeScript(t *testing.T, name string, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, name)
	script := fmt.Sprintf("#!/bin/sh\nexit %d\n", exitCode)
	require.NoError(t, os.WriteFile(bin, []byte(script), 0o755))
	return bin
}

func writeFakeGo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "go")
	script := `#!/bin/sh
case "$2" in
  GOROOT) echo "/fake/goroot" ;;
  GOPATH) echo "/fake/gopath" ;;
  GOMODCACHE) echo "/fake/gomodcache" ;;
  *) echo "" ;;
esac
`
	require.NoError(t, os.WriteFile(bin, []byte(script), 0o755))
	return bin
}

func writeFakeGoPartial(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "go")
	script := `#!/bin/sh
case "$2" in
  GOROOT) echo "/fake/goroot" ;;
  *) echo "" ;;
esac
`
	require.NoError(t, os.WriteFile(bin, []byte(script), 0o755))
	return bin
}

func TestExecute_MissingPackage(t *testing.T) {
	resetFlags(t)
	setArgs(t, []string{"test", "--out", "/tmp/out.sarif"})

	code := execute()

	assert.Equal(t, 2, code)
}

func TestExecute_MissingOut(t *testing.T) {
	resetFlags(t)
	setArgs(t, []string{"test", "--package", "./internal/order"})

	code := execute()

	assert.Equal(t, 2, code)
}

func TestExecute_RunError(t *testing.T) {
	resetFlags(t)
	t.Setenv("PATH", t.TempDir())
	dir := t.TempDir()
	setArgs(t, []string{"test", "--package", "./pkg", "--out", filepath.Join(dir, "out.sarif")})

	code := execute()

	assert.Equal(t, 1, code)
}

func TestExecute_Success(t *testing.T) {
	lint := writeFakeScript(t, "golangci-lint", 0)
	resetFlags(t)
	dir := t.TempDir()
	setArgs(t, []string{"test", "--golangci-lint", lint, "--package", "./pkg", "--out", filepath.Join(dir, "out.sarif")})

	code := execute()

	assert.Equal(t, 0, code)
}

func TestRun_MkdirAllError(t *testing.T) {
	lint := writeFakeScript(t, "golangci-lint", 0)

	err := run(lint, "", "./pkg", "/dev/null/impossible/out.sarif", false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "create output dir")
}

func TestRun_LintNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	dir := t.TempDir()

	err := run("", "", "./pkg", filepath.Join(dir, "out.sarif"), false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRun_LintFoundInPath(t *testing.T) {
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "golangci-lint")
	require.NoError(t, os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755))
	t.Setenv("PATH", tmp)
	dir := t.TempDir()

	err := run("", "", "./pkg", filepath.Join(dir, "out.sarif"), false)

	require.NoError(t, err)
}

func TestRun_CommandError(t *testing.T) {
	lint := writeFakeScript(t, "golangci-lint", 1)
	dir := t.TempDir()

	err := run(lint, "", "./pkg", filepath.Join(dir, "out.sarif"), false)

	require.Error(t, err)
}

func TestRun_WithConfigFile(t *testing.T) {
	lint := writeFakeScript(t, "golangci-lint", 0)
	dir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".golangci.yml"), []byte("linters:\n"), 0o644))

	err = run(lint, "", "./pkg", filepath.Join(dir, "out", "result.sarif"), false)

	require.NoError(t, err)
}

func TestRun_WithGoBinary(t *testing.T) {
	lint := writeFakeScript(t, "golangci-lint", 0)
	goBin := writeFakeGo(t)
	dir := t.TempDir()

	err := run(lint, goBin, "./pkg", filepath.Join(dir, "out.sarif"), false)

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

func TestCommandEnv_GOPATHResolvesFromGoBinary(t *testing.T) {
	env := commandEnv(t.TempDir(), "")

	envMap := envToMap(env)
	goPath := envMap["GOPATH"]
	goModCache := envMap["GOMODCACHE"]
	assert.NotEmpty(t, goPath, "GOPATH must be set")
	assert.NotEmpty(t, goModCache, "GOMODCACHE must be set")
}

func TestCommandEnv_WithExplicitGoBinary(t *testing.T) {
	goBin := writeFakeGo(t)
	env := commandEnv(t.TempDir(), goBin)

	envMap := envToMap(env)
	assert.Equal(t, "/fake/goroot", envMap["GOROOT"])
	assert.Equal(t, "/fake/gopath", envMap["GOPATH"])
	assert.Equal(t, "/fake/gomodcache", envMap["GOMODCACHE"])
	assert.Contains(t, envMap["PATH"], filepath.Dir(goBin))
}

func TestCommandEnv_WithoutExplicitGoBinary(t *testing.T) {
	env := commandEnv(t.TempDir(), "")

	envMap := envToMap(env)
	assert.NotEmpty(t, envMap["GOCACHE"], "GOCACHE must always be set")
	assert.NotEmpty(t, envMap["GOLANGCI_LINT_CACHE"], "GOLANGCI_LINT_CACHE must always be set")
}

func TestCommandEnv_FallbackGOPATHAndGOMODCACHE(t *testing.T) {
	goBin := writeFakeGoPartial(t)
	env := commandEnv(t.TempDir(), goBin)

	envMap := envToMap(env)
	assert.Equal(t, "/fake/goroot", envMap["GOROOT"])
	assert.NotEmpty(t, envMap["GOPATH"], "GOPATH should fall back to defaultGoPath")
	assert.Contains(t, envMap["GOMODCACHE"], "pkg/mod", "GOMODCACHE should fall back to GOPATH/pkg/mod")
}

func TestCommandEnv_PathNotDuplicated(t *testing.T) {
	goBin := writeFakeGo(t)
	goDir := filepath.Dir(goBin)
	t.Setenv("PATH", goDir+":/usr/bin")
	env := commandEnv(t.TempDir(), goBin)

	envMap := envToMap(env)
	count := strings.Count(envMap["PATH"], goDir)
	assert.Equal(t, 1, count, "Go binary dir should appear once in PATH")
}

func TestCommandEnv_EmptyPATH(t *testing.T) {
	goBin := writeFakeGo(t)
	goDir := filepath.Dir(goBin)
	t.Setenv("PATH", "")
	env := commandEnv(t.TempDir(), goBin)

	envMap := envToMap(env)
	assert.Equal(t, goDir, envMap["PATH"])
}

func TestCommandEnv_HasGOCACHE(t *testing.T) {
	env := commandEnv("/tmp/cache", "")

	envMap := envToMap(env)
	assert.NotEmpty(t, envMap["GOCACHE"])
}

func TestCommandEnv_HasGOLANGCI_LINT_CACHE(t *testing.T) {
	cacheDir := t.TempDir()
	env := commandEnv(cacheDir, "")

	envMap := envToMap(env)
	assert.Equal(t, cacheDir, envMap["GOLANGCI_LINT_CACHE"])
}

func TestGoRootFor_WithWorkingGoBinary(t *testing.T) {
	goBin := writeFakeGo(t)

	got := goRootFor(goBin)

	assert.Equal(t, "/fake/goroot", got)
}

func TestGoRootFor_FallsBackToParentDir(t *testing.T) {
	badBin := writeFakeScript(t, "go", 1)

	got := goRootFor(badBin)

	assert.Equal(t, filepath.Dir(filepath.Dir(badBin)), got)
}

func TestGoEnv_ReturnsValue(t *testing.T) {
	goBin := writeFakeGo(t)

	got := goEnv(goBin, "GOROOT")

	assert.Equal(t, "/fake/goroot", got)
}

func TestGoEnv_ReturnsEmptyOnError(t *testing.T) {
	badBin := writeFakeScript(t, "go", 1)

	got := goEnv(badBin, "GOROOT")

	assert.Equal(t, "", got)
}

func TestGoEnv_EmptyKeyDefaultsToGOROOT(t *testing.T) {
	goBin := writeFakeGo(t)

	got := goEnv(goBin, "")

	assert.Equal(t, "/fake/goroot", got)
}

func TestWithoutEnv_RemovesMatchingKey(t *testing.T) {
	env := []string{"GOROOT=/usr/local/go", "HOME=/home/user", "GOPATH=/go"}

	got := withoutEnv(env, "GOROOT")

	assert.Equal(t, []string{"HOME=/home/user", "GOPATH=/go"}, got)
}

func TestWithoutEnv_NoMatch(t *testing.T) {
	env := []string{"HOME=/home/user", "PATH=/usr/bin"}

	got := withoutEnv(env, "GOROOT")

	assert.Equal(t, []string{"HOME=/home/user", "PATH=/usr/bin"}, got)
}

func TestWithoutEnv_EmptySlice(t *testing.T) {
	got := withoutEnv([]string{}, "GOROOT")

	assert.Empty(t, got)
}

func TestWithoutEnv_DoesNotMatchPrefix(t *testing.T) {
	env := []string{"GOROOT_FINAL=/somewhere", "GOROOT=/usr/local/go"}

	got := withoutEnv(env, "GOROOT")

	assert.Equal(t, []string{"GOROOT_FINAL=/somewhere"}, got)
}

func TestSanitizedEnv_StripsGOROOT(t *testing.T) {
	t.Setenv("GOROOT", "/old/goroot")

	env := sanitizedEnv()

	for _, item := range env {
		if strings.HasPrefix(item, "GOROOT=") {
			t.Fatal("GOROOT should be stripped from sanitized env")
		}
	}
}

func TestSanitizedEnv_StripsGOPATH(t *testing.T) {
	t.Setenv("GOPATH", "/old/gopath")

	env := sanitizedEnv()

	for _, item := range env {
		if strings.HasPrefix(item, "GOPATH=") {
			t.Fatal("GOPATH should be stripped from sanitized env")
		}
	}
}

func TestSanitizedEnv_StripsGOMODCACHE(t *testing.T) {
	t.Setenv("GOMODCACHE", "/old/modcache")

	env := sanitizedEnv()

	for _, item := range env {
		if strings.HasPrefix(item, "GOMODCACHE=") {
			t.Fatal("GOMODCACHE should be stripped from sanitized env")
		}
	}
}

func TestSanitizedEnv_StripsGOTOOLCHAIN(t *testing.T) {
	t.Setenv("GOTOOLCHAIN", "go1.22")

	env := sanitizedEnv()

	envMap := envToMap(env)
	assert.Equal(t, "auto", envMap["GOTOOLCHAIN"])
}

func TestSanitizedEnv_KeepsValidGOPROXY(t *testing.T) {
	t.Setenv("GOPROXY", "https://custom.proxy.com,direct")

	env := sanitizedEnv()

	envMap := envToMap(env)
	assert.Equal(t, "https://custom.proxy.com,direct", envMap["GOPROXY"])
}

func TestSanitizedEnv_ReplacesInvalidGOPROXY(t *testing.T) {
	t.Setenv("GOPROXY", ", , ,")

	env := sanitizedEnv()

	envMap := envToMap(env)
	assert.Equal(t, "https://proxy.golang.org,direct", envMap["GOPROXY"])
}

func TestSanitizedEnv_KeepsGOSUMDB(t *testing.T) {
	t.Setenv("GOSUMDB", "sum.custom.org")

	env := sanitizedEnv()

	envMap := envToMap(env)
	assert.Equal(t, "sum.golang.org", envMap["GOSUMDB"])
}

func TestSanitizedEnv_DropsEmptyGOSUMDB(t *testing.T) {
	t.Setenv("GOSUMDB", "  ")

	env := sanitizedEnv()

	envMap := envToMap(env)
	assert.Equal(t, "sum.golang.org", envMap["GOSUMDB"])
}

func TestSanitizedEnv_InjectsHOME(t *testing.T) {
	t.Setenv("HOME", "/my/home")

	env := sanitizedEnv()

	envMap := envToMap(env)
	assert.Equal(t, "/my/home", envMap["HOME"])
}

func TestSanitizedEnv_InjectsHOMEWhenMissing(t *testing.T) {
	t.Setenv("HOME", "")

	home, err := homeDir()
	if err != nil {
		t.Skip("user.Current() unavailable in this environment")
	}

	env := sanitizedEnv()

	envMap := envToMap(env)
	assert.Equal(t, home, envMap["HOME"])
}

func TestDefaultGoPath_ReturnsPath(t *testing.T) {
	got := defaultGoPath()

	assert.NotEmpty(t, got)
	assert.True(t, strings.HasSuffix(got, "go"), "should end with 'go'")
}

func TestDefaultGoPath_UsesUserHomeDir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("UserHomeDir unavailable")
	}

	got := defaultGoPath()

	assert.Equal(t, filepath.Join(home, "go"), got)
}

func TestValidGoProxy_ValidURL(t *testing.T) {
	assert.True(t, validGoProxy("https://proxy.golang.org,direct"))
}

func TestValidGoProxy_Empty(t *testing.T) {
	assert.False(t, validGoProxy(""))
}

func TestValidGoProxy_OnlyCommasAndSpaces(t *testing.T) {
	assert.False(t, validGoProxy(", , ,"))
}

func TestValidGoProxy_DirectOnly(t *testing.T) {
	assert.True(t, validGoProxy("direct"))
}

func TestFindGoBinary_FindsInPath(t *testing.T) {
	got := findGoBinary()

	assert.NotEmpty(t, got, "Go should be available in the test environment")
}

func TestFindGoBinary_EmptyWhenNotInPath(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	got := findGoBinary()

	if got != "" {
		for _, candidate := range []string{
			"/opt/homebrew/bin/go",
			"/usr/local/go/bin/go",
			"/usr/bin/go",
		} {
			if got == candidate {
				t.Skipf("Go found at well-known path %s", candidate)
			}
		}
		t.Fatalf("unexpected Go binary found at %s", got)
	}
}

func TestHomeDir_ReturnsNonEmpty(t *testing.T) {
	dir, err := homeDir()

	require.NoError(t, err)
	assert.NotEmpty(t, dir)
}

func TestHomeDir_UsesEnvFirst(t *testing.T) {
	t.Setenv("HOME", "/fake/home")

	dir, err := homeDir()

	require.NoError(t, err)
	assert.Equal(t, "/fake/home", dir)
}

func TestHomeDir_FallsBackWithoutHOME(t *testing.T) {
	t.Setenv("HOME", "")

	dir, err := homeDir()
	if err != nil {
		t.Skip("user.Current() unavailable in this environment")
	}

	assert.NotEmpty(t, dir)
}

func TestResolveBazelExternal_ExistingPath(t *testing.T) {
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "tool")
	require.NoError(t, os.WriteFile(bin, []byte("x"), 0o755))

	got := resolveBazelExternal(bin)

	assert.Equal(t, bin, got)
}

func TestResolveBazelExternal_NonExternalPrefix(t *testing.T) {
	got := resolveBazelExternal("/nonexistent/path/to/tool")

	assert.Equal(t, "/nonexistent/path/to/tool", got)
}

func TestResolveBazelExternal_ExternalPrefixAlternate(t *testing.T) {
	tmp := t.TempDir()
	workDir := filepath.Join(tmp, "a", "b")
	require.NoError(t, os.MkdirAll(workDir, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "external", "foo"), 0o755))
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(workDir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	got := resolveBazelExternal("external/foo")

	assert.Equal(t, filepath.Join("..", "..", "external", "foo"), got)
}

func TestResolveBazelExternal_ExternalPrefixGlobMatch(t *testing.T) {
	tmp := t.TempDir()
	workDir := filepath.Join(tmp, "a", "b")
	require.NoError(t, os.MkdirAll(workDir, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "external", "prefix~foo"), 0o755))
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(workDir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	got := resolveBazelExternal("external/foo")

	assert.Equal(t, filepath.Join("..", "..", "external", "prefix~foo"), got)
}

func TestResolveBazelExternal_ExternalPrefixNoMatch(t *testing.T) {
	tmp := t.TempDir()
	workDir := filepath.Join(tmp, "a", "b")
	require.NoError(t, os.MkdirAll(workDir, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "external"), 0o755))
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(workDir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	got := resolveBazelExternal("external/nomatch")

	assert.Equal(t, "external/nomatch", got)
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
