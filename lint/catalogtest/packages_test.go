package catalog_test

import (
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// consumerPackages lists, per module, the Bazel packages a consumer loads or
// references (labels in the catalog, toolchains, aspects, macros). It is explicit
// on purpose: adding a package a consumer can reach means adding it here, and so
// to the data of this test.
//
// Why this exists: a release (IS_RELEASE) makes rules_go and gazelle dev
// dependencies, so they are not in a consumer's module graph. A BUILD file that
// loads from one fails to load for the consumer, and with it every label in that
// package. gavel-tools 0.4.0 shipped exactly that in //lint, making
// @gavel_tools//lint:catalog.yaml unusable.
var consumerPackages = map[string][]string{
	coreModule: {
		"", "lint", "lint/aspects", "lint/toolchain", "lint/toolchain/source",
	},
	"gavel_tools_go": {
		"", "lint/aspects", "lint/lang/go/golangci_lint",
	},
	"gavel_tools_java": {
		"", "lint/aspects", "lint/lang/java/error_prone", "lint/lang/java/pmd", "lint/lang/java/spotbugs",
	},
	"gavel_tools_python": {
		"", "lint/aspects", "lint/lang/python/bandit", "lint/lang/python/ruff",
	},
	"gavel_tools_rust": {
		"", "lint/aspects",
	},
	"gavel_tools_rust_rs": {
		"", "lint", "lint/aspects",
	},
	"gavel_tools_typescript": {
		"", "lint/aspects", "lint/lang/typescript/eslint",
	},
	"gavel_tools_web": {
		"", "macros",
	},
}

// repositoryGeneratedByExtensions are repos a consumer-facing BUILD file may load
// from although they are not bazel_deps: Bazel's own, and the ones a non-dev
// module extension of the same module generates.
var repositoryGeneratedByExtensions = []string{"bazel_tools", "gavel_eslint"}

// buildFilePath is where the data of this test exposes a package's BUILD file.
func buildFilePath(t *testing.T, module, pkg string) string {
	t.Helper()
	if module == coreModule {
		return path.Join("../..", pkg, "BUILD.bazel")
	}
	return globOne(t, "../../../"+module+"+*/"+path.Join(pkg, "BUILD.bazel"))
}

func moduleFilePath(t *testing.T, module string) string {
	t.Helper()
	if module == coreModule {
		return moduleFiles[coreModule]
	}
	return globOne(t, "../../../"+module+"+*/MODULE.bazel")
}

// devRepos returns the apparent repo names of the bazel_deps a MODULE.bazel
// declares as dev dependencies (True, or the IS_RELEASE switch), and the ones it
// declares for consumers.
func devRepos(t *testing.T, modulePath string) (dev, regular map[string]bool) {
	t.Helper()
	text, err := os.ReadFile(modulePath)
	require.NoError(t, err)
	dev, regular = map[string]bool{}, map[string]bool{}
	for _, m := range regexp.MustCompile(`(?s)\bbazel_dep\((.*?)\)`).FindAllStringSubmatch(string(text), -1) {
		name := regexp.MustCompile(`\bname\s*=\s*"([^"]+)"`).FindStringSubmatch(m[1])
		require.NotNil(t, name, "bazel_dep without a name in %s", modulePath)
		repo := name[1]
		if alias := regexp.MustCompile(`\brepo_name\s*=\s*"([^"]+)"`).FindStringSubmatch(m[1]); alias != nil {
			repo = alias[1]
		}
		isDev := regexp.MustCompile(`\bdev_dependency\s*=`).MatchString(m[1]) &&
			!regexp.MustCompile(`\bdev_dependency\s*=\s*False\b`).MatchString(m[1])
		if isDev {
			dev[repo] = true
		} else {
			regular[repo] = true
		}
	}
	return dev, regular
}

var loadLabel = regexp.MustCompile(`(?m)^\s*load\(\s*"([^"]+)"`)

// repoOf returns the apparent repo a load label names, "" for a label of the
// package's own repo.
func repoOf(label string) string {
	if !strings.HasPrefix(label, "@") {
		return ""
	}
	return strings.TrimPrefix(strings.SplitN(label, "//", 2)[0], "@")
}

func TestConsumerFacingPackages_LoadOnlyFromRepositoriesAConsumerHas(t *testing.T) {
	for module, packages := range consumerPackages {
		dev, regular := devRepos(t, moduleFilePath(t, module))
		for _, pkg := range packages {
			file := buildFilePath(t, module, pkg)
			text, err := os.ReadFile(file)
			require.NoError(t, err)
			for _, m := range loadLabel.FindAllStringSubmatch(string(text), -1) {
				repo := repoOf(m[1])
				where := module + "//" + pkg + ": load(" + m[1] + ")"
				switch {
				case repo == "" || repo == module:
				case dev[repo]:
					assert.Fail(t, where, "@%s is a dev dependency of %s: it is absent from a consumer's module graph, so this package would not load for them. Move whatever needs it into a package no consumer references", repo, module)
				case regular[repo] || contains(repositoryGeneratedByExtensions, repo):
				default:
					assert.Fail(t, where, "@%s is neither a non-dev bazel_dep of %s nor a known extension repo", repo, module)
				}
			}
		}
	}
}

func TestConsumerFacingPackages_ListNamesEveryModule(t *testing.T) {
	var modules []string
	for module := range consumerPackages {
		modules = append(modules, module)
	}
	sort.Strings(modules)
	assert.Equal(t, []string{
		"gavel_tools", "gavel_tools_go", "gavel_tools_java", "gavel_tools_python",
		"gavel_tools_rust", "gavel_tools_rust_rs", "gavel_tools_typescript", "gavel_tools_web",
	}, modules)
}
