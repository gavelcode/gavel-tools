package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/gavelcode/gavel-tools/lint/sarif"
)

const (
	exitUsageError = 2
	dirPermission  = 0o755
)

func main() { os.Exit(execute()) }

func execute() int {
	binary := flag.String("golangci-lint", "", "Path to the pinned golangci-lint binary")
	driver := flag.String("driver", "", "Path to the static gopackagesdriver binary")
	manifest := flag.String("manifest", "", "Path to the pkg.json manifest the driver reads")
	goBinary := flag.String("go", "", "Path to the Go SDK binary, used to derive GOROOT")
	pkg := flag.String("package", "", "Go package directory to lint")
	out := flag.String("out", "", "SARIF output path")
	skipTests := flag.Bool("skip-tests", false, "Exclude test files from analysis")
	flag.Parse()

	if *pkg == "" {
		fmt.Fprintln(os.Stderr, "missing --package")
		return exitUsageError
	}
	if *out == "" {
		fmt.Fprintln(os.Stderr, "missing --out")
		return exitUsageError
	}

	if err := run(*binary, *driver, *manifest, *goBinary, *pkg, *out, *skipTests); err != nil {
		fmt.Fprintf(os.Stderr, "run golangci-lint: %v\n", err)
		return 1
	}
	return 0
}

func run(binary, driver, manifest, goBinary, pkg, out string, skipTests bool) (err error) {
	if absOut, absErr := filepath.Abs(out); absErr == nil {
		out = absOut
	}
	if err := os.MkdirAll(filepath.Dir(out), dirPermission); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	if binary == "" {
		bin, lookErr := exec.LookPath("golangci-lint")
		if lookErr != nil {
			return errors.New("golangci-lint not found in PATH and --golangci-lint was not provided")
		}
		binary = bin
	}

	cacheDir, err := os.MkdirTemp("", "gavel-golangci-cache-*")
	if err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}
	defer func() {
		if removeErr := os.RemoveAll(cacheDir); removeErr != nil && err == nil {
			err = removeErr
		}
	}()

	configFile := ""
	if _, statErr := os.Stat(".golangci.yml"); statErr == nil {
		configFile = ".golangci.yml"
	}
	args := buildLintArgs("./"+filepath.ToSlash(pkg), out, skipTests, configFile)
	cmd := exec.Command(binary, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = driverEnv(cacheDir, driver, manifest, goBinary)
	if runErr := cmd.Run(); runErr != nil {
		return sarif.WriteFailed(out, "golangci-lint", fmt.Sprintf("golangci-lint failed to run: %v", runErr))
	}
	return sarif.MarkSuccessful(out)
}

func buildLintArgs(pkg, out string, skipTests bool, configFile string) []string {
	args := []string{
		"run",
		pkg,
		"--issues-exit-code=0",
		"--allow-parallel-runners",
		"--output.sarif.path=" + out,
	}
	if skipTests {
		args = append(args, "--tests=false")
	}
	if configFile != "" {
		args = append(args, "--config="+configFile)
	}
	return args
}

// driverEnv builds golangci-lint's environment for hermetic, sandboxed
// analysis: the static packages driver supplies the whole build graph from the
// manifest, so the network is off and the only Go toolchain is the pinned SDK
// behind GOROOT. golangci-lint inherits these and forwards them to the driver
// subprocess it spawns.
func driverEnv(cacheDir, driver, manifest, goBinary string) []string {
	env := []string{
		"PATH=" + binPath(goBinary),
		"HOME=" + cacheDir,
		"GOPROXY=off",
		"GOSUMDB=off",
		"GOTOOLCHAIN=local",
		"GOFLAGS=-mod=mod",
		"GOCACHE=" + filepath.Join(cacheDir, "build"),
		"GOLANGCI_LINT_CACHE=" + filepath.Join(cacheDir, "lint"),
	}
	if driver != "" {
		env = append(env, "GOPACKAGESDRIVER="+absOrSelf(driver))
	}
	if manifest != "" {
		env = append(env, "GAVEL_PKG_JSON_MANIFEST="+absOrSelf(manifest))
	}
	if goBinary != "" {
		env = append(env, "GOROOT="+goRoot(goBinary))
	}
	return env
}

// binPath puts the SDK's bin directory first so golangci-lint's auxiliary
// `go env` calls (gci formatter, gomod salt) resolve the pinned toolchain
// rather than failing on an empty PATH.
func binPath(goBinary string) string {
	system := "/usr/bin:/bin"
	if goBinary == "" {
		return system
	}
	return filepath.Dir(absOrSelf(goBinary)) + ":" + system
}

// goRoot derives GOROOT from the SDK go binary at <root>/bin/go.
func goRoot(goBinary string) string {
	return filepath.Dir(filepath.Dir(absOrSelf(goBinary)))
}

func absOrSelf(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}
