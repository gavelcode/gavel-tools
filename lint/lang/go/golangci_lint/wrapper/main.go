package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/gavelcode/gavel-tools/lint/sarif"
)

const (
	exitUsageError = 2
	dirPermission  = 0o755
)

func main() { os.Exit(execute()) }

func execute() int {
	binary := flag.String("golangci-lint", "", "Path to the pinned golangci-lint binary")
	goBinary := flag.String("go", "", "Path to the Go binary used by golangci-lint")
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

	if err := run(*binary, *goBinary, *pkg, *out, *skipTests); err != nil {
		fmt.Fprintf(os.Stderr, "run golangci-lint: %v\n", err)
		return 1
	}
	return 0
}

func run(binary, goBinary, pkg, out string, skipTests bool) (err error) {
	absOut, err := filepath.Abs(out)
	if err == nil {
		out = absOut
	}
	if err := os.MkdirAll(filepath.Dir(out), dirPermission); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	if binary == "" {
		bin, err := exec.LookPath("golangci-lint")
		if err != nil {
			return errors.New("golangci-lint not found in PATH and --golangci-lint was not provided")
		}
		binary = bin
	}
	binary = resolveBazelExternal(binary)

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
	if _, err := os.Stat(".golangci.yml"); err == nil {
		configFile = ".golangci.yml"
	}
	args := buildLintArgs("./"+filepath.ToSlash(pkg), out, skipTests, configFile)
	cmd := exec.Command(binary, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = commandEnv(cacheDir, goBinary)
	if err := cmd.Run(); err != nil {
		return sarif.WriteFailed(out, "golangci-lint", fmt.Sprintf("golangci-lint failed to run: %v", err))
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

func commandEnv(cacheDir, goBinary string) []string {
	env := append(sanitizedEnv(),
		"GOCACHE="+filepath.Join(os.TempDir(), "gavel-go-build-cache"),
		"GOLANGCI_LINT_CACHE="+cacheDir,
	)
	if goBinary == "" {
		goBinary = findGoBinary()
	}
	if goBinary == "" {
		return env
	}

	goBinary = resolveBazelExternal(goBinary)
	absGoBinary, err := filepath.Abs(goBinary)
	if err == nil {
		goBinary = absGoBinary
	}
	goDir := filepath.Dir(goBinary)
	goRoot := goRootFor(goBinary)
	env = append(env, "GOROOT="+goRoot)

	goPath := goEnv(goBinary, "GOPATH")
	if goPath == "" {
		goPath = defaultGoPath()
	}
	env = append(env, "GOPATH="+goPath)

	goModCache := goEnv(goBinary, "GOMODCACHE")
	if goModCache == "" {
		goModCache = filepath.Join(goPath, "pkg", "mod")
	}
	env = append(env, "GOMODCACHE="+goModCache)

	path := os.Getenv("PATH")
	if path == "" {
		path = goDir
	} else if !strings.Contains(path, goDir) {
		path = goDir + string(os.PathListSeparator) + path
	}
	env = append(env, "PATH="+path)
	return env
}

func goRootFor(goBinary string) string {
	if root := goEnv(goBinary, "GOROOT"); root != "" {
		return root
	}
	return filepath.Dir(filepath.Dir(goBinary))
}

func goEnv(goBinary, key string) string {
	cmd := exec.Command(goBinary, "env", "GOROOT")
	if key != "" {
		cmd = exec.Command(goBinary, "env", key)
	}
	cmd.Env = append(withoutEnv(withoutEnv(os.Environ(), "GOROOT"), "GOTOOLCHAIN"), "GOTOOLCHAIN=local")
	out, err := cmd.Output()
	if err == nil {
		return strings.TrimSpace(string(out))
	}
	return ""
}

func withoutEnv(env []string, key string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env))
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func sanitizedEnv() []string {
	env := make([]string, 0, len(os.Environ())+1)
	resolvedHome := os.Getenv("HOME")
	if resolvedHome == "" {
		if home, err := homeDir(); err == nil {
			resolvedHome = home
		}
	}
	hasValidGoProxy := false
	for _, item := range os.Environ() {
		if val, ok := strings.CutPrefix(item, "GOPROXY="); ok {
			if validGoProxy(val) {
				env = append(env, item)
				hasValidGoProxy = true
			}
			continue
		}
		if val, ok := strings.CutPrefix(item, "GOSUMDB="); ok {
			if strings.TrimSpace(val) != "" {
				env = append(env, item)
			}
			continue
		}
		if strings.HasPrefix(item, "GOROOT=") {
			continue
		}
		if strings.HasPrefix(item, "GOPATH=") {
			continue
		}
		if strings.HasPrefix(item, "GOMODCACHE=") {
			continue
		}
		if strings.HasPrefix(item, "GOTOOLCHAIN=") {
			continue
		}
		if strings.HasPrefix(item, "HOME=") {
			continue
		}
		env = append(env, item)
	}
	if resolvedHome != "" {
		env = append(env, "HOME="+resolvedHome)
	}
	if !hasValidGoProxy {
		env = append(env, "GOPROXY=https://proxy.golang.org,direct")
	}
	env = append(env, "GOSUMDB=sum.golang.org")
	env = append(env, "GOTOOLCHAIN=auto")
	return env
}

func defaultGoPath() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, "go")
	}
	if home, err := homeDir(); err == nil {
		return filepath.Join(home, "go")
	}
	return filepath.Join(os.TempDir(), "gavel-gopath")
}

func homeDir() (string, error) {
	if h := os.Getenv("HOME"); h != "" {
		return h, nil
	}
	u, err := user.Current()
	if err == nil && u.HomeDir != "" {
		return u.HomeDir, nil
	}
	return "", fmt.Errorf("cannot determine home directory")
}

func validGoProxy(value string) bool {
	if value == "" {
		return false
	}
	for part := range strings.SplitSeq(value, ",") {
		if strings.TrimSpace(part) != "" {
			return true
		}
	}
	return false
}

func findGoBinary() string {
	if path, err := exec.LookPath("go"); err == nil {
		return path
	}
	for _, candidate := range []string{
		"/opt/homebrew/bin/go",
		"/usr/local/go/bin/go",
		"/usr/bin/go",
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func resolveBazelExternal(path string) string {
	if _, err := os.Stat(path); err == nil {
		return path
	}
	if suffix, ok := strings.CutPrefix(path, "external/"); ok {
		alternate := filepath.Join("..", "..", path)
		if _, err := os.Stat(alternate); err == nil {
			return alternate
		}
		matches, err := filepath.Glob(filepath.Join("..", "..", "external", "*"+suffix))
		if err == nil && len(matches) > 0 {
			return matches[0]
		}
	}
	return path
}
