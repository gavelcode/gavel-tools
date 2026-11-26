package wrapper

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

	// DriverLauncherName is the name the wrapper gives the symlink to its own
	// binary that it hands golangci-lint as GOPACKAGESDRIVER. The multi-call
	// binary recognises that argv[0] and runs the packages driver.
	DriverLauncherName = "gavel-packagesdriver"
)

// Main runs the golangci tool with args (the tool name already stripped) and
// returns the process exit code.
func Main(arguments []string) int {
	flags := flag.NewFlagSet("golangci", flag.ExitOnError)
	binary := flags.String("golangci-lint", "", "Path to the pinned golangci-lint binary")
	driver := flags.String("driver", "", "Path to a gopackagesdriver executable (default: this binary under its launcher name)")
	manifest := flags.String("manifest", "", "Path to the pkg.json manifest the driver reads")
	goBinary := flags.String("go", "", "Path to the Go SDK binary, used to derive GOROOT")
	stdlibDir := flags.String("stdlib-pkg-dir", "", "Directory of compiled stdlib .a archives, used as export data")
	packageFlag := flags.String("package", "", "Go package directory to lint")
	outputFlag := flags.String("out", "", "SARIF output path")
	skipTests := flags.Bool("skip-tests", false, "Exclude test files from analysis")
	_ = flags.Parse(arguments)

	if *packageFlag == "" {
		fmt.Fprintln(os.Stderr, "missing --package")
		return exitUsageError
	}
	if *outputFlag == "" {
		fmt.Fprintln(os.Stderr, "missing --out")
		return exitUsageError
	}

	if err := run(*binary, *driver, *manifest, *goBinary, *stdlibDir, *packageFlag, *outputFlag, *skipTests); err != nil {
		fmt.Fprintf(os.Stderr, "run golangci-lint: %v\n", err)
		return 1
	}
	return 0
}

func run(binary, driver, manifest, goBinary, stdlibDir, packageDir, outputPath string, skipTests bool) (err error) {
	if absOut, absErr := filepath.Abs(outputPath); absErr == nil {
		outputPath = absOut
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), dirPermission); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	binary, err = resolveBinary(binary)
	if err != nil {
		return err
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
	arguments := buildLintArgs("./"+filepath.ToSlash(packageDir), outputPath, skipTests, configFile)
	driver, err = orSelfLauncher(driver, cacheDir)
	if err != nil {
		return err
	}
	cmd := exec.Command(binary, arguments...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = driverEnv(cacheDir, driver, manifest, goBinary, stdlibDir)
	if runErr := cmd.Run(); runErr != nil {
		return sarif.WriteFailed(outputPath, "golangci-lint", fmt.Sprintf("golangci-lint failed to run: %v", runErr))
	}
	return sarif.MarkSuccessful(outputPath)
}

// resolveBinary keeps an explicit golangci-lint path, otherwise looks it up in PATH.
func resolveBinary(binary string) (string, error) {
	if binary != "" {
		return binary, nil
	}
	found, err := exec.LookPath("golangci-lint")
	if err != nil {
		return "", errors.New("golangci-lint not found in PATH and --golangci-lint was not provided")
	}
	return found, nil
}

// orSelfLauncher keeps an explicit driver, otherwise makes this binary its own.
func orSelfLauncher(driver, cacheDir string) (string, error) {
	if driver != "" {
		return driver, nil
	}
	launcher, err := linkSelfAs(cacheDir, DriverLauncherName)
	if err != nil {
		return "", fmt.Errorf("create packages driver launcher: %w", err)
	}
	return launcher, nil
}

// linkSelfAs symlinks this executable as dir/name and returns the link. A
// GOPACKAGESDRIVER must be a plain executable path, so the multi-call binary
// serves as its own driver by being invoked under a different argv[0].
func linkSelfAs(directory, launcherName string) (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	if abs, absErr := filepath.Abs(executable); absErr == nil {
		executable = abs
	}
	launcher := filepath.Join(directory, launcherName)
	if err := os.Symlink(executable, launcher); err != nil {
		return "", err
	}
	return launcher, nil
}

func buildLintArgs(packageDir, outputPath string, skipTests bool, configFile string) []string {
	arguments := []string{
		"run",
		packageDir,
		"--issues-exit-code=0",
		"--allow-parallel-runners",
		"--output.sarif.path=" + outputPath,
	}
	if skipTests {
		arguments = append(arguments, "--tests=false")
	}
	if configFile != "" {
		arguments = append(arguments, "--config="+configFile)
	}
	return arguments
}

// driverEnv builds golangci-lint's environment for hermetic, sandboxed
// analysis: the static packages driver supplies the whole build graph from the
// manifest, so the network is off and the only Go toolchain is the pinned SDK
// behind GOROOT. golangci-lint inherits these and forwards them to the driver
// subprocess it spawns.
func driverEnv(cacheDir, driver, manifest, goBinary, stdlibDir string) []string {
	environment := []string{
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
		environment = append(environment, "GOPACKAGESDRIVER="+absOrSelf(driver))
	}
	if manifest != "" {
		environment = append(environment, "GAVEL_PKG_JSON_MANIFEST="+absOrSelf(manifest))
	}
	if goBinary != "" {
		environment = append(environment, "GOROOT="+goRoot(goBinary))
	}
	if stdlibDir != "" {
		environment = append(environment, "GAVEL_STDLIB_PKG_DIR="+absOrSelf(stdlibDir))
	}
	return environment
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
