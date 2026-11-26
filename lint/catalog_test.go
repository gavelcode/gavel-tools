package catalog_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type catalogFile struct {
	Version    int                    `yaml:"version"`
	AspectsBzl string                 `yaml:"aspects_bzl"`
	Languages  map[string][]toolEntry `yaml:"languages"`
}

type toolEntry struct {
	Name        string    `yaml:"name"`
	Module      string    `yaml:"module"`
	AspectsBzl  string    `yaml:"aspects_bzl"`
	Aspect      string    `yaml:"aspect"`
	SARIFSuffix string    `yaml:"sarif_suffix"`
	BuildFlags  []string  `yaml:"build_flags"`
	Binary      string    `yaml:"binary"`
	Variants    []variant `yaml:"variants"`
}

// variant is an alternative way to run a tool, selected by a Bazel module the
// consumer already depends on (see the catalog.yaml header).
type variant struct {
	WhenBazelDep string   `yaml:"when_bazel_dep"`
	Module       string   `yaml:"module"`
	AspectsBzl   string   `yaml:"aspects_bzl"`
	Aspect       string   `yaml:"aspect"`
	BuildFlags   []string `yaml:"build_flags"`
}

func loadCatalog(t *testing.T) catalogFile {
	t.Helper()
	data, err := os.ReadFile("catalog.yaml")
	require.NoError(t, err)
	var catalog catalogFile
	require.NoError(t, yaml.Unmarshal(data, &catalog))
	return catalog
}

// coreModule is the module a tool belongs to when its entry names none.
const coreModule = "gavel_tools"

// moduleDefs maps each Bazel module the catalog can name to where its
// lint/aspects/defs.bzl is visible to this test: the core module is this
// package's own tree, the others are runfiles of the dev-only dependencies
// declared in the root MODULE.bazel (the `+` keeps gavel_tools_rust from also
// matching gavel_tools_rust_rs).
var moduleDefs = map[string]string{
	coreModule:               "aspects/defs.bzl",
	"gavel_tools_go":         "../../gavel_tools_go+*/lint/aspects/defs.bzl",
	"gavel_tools_java":       "../../gavel_tools_java+*/lint/aspects/defs.bzl",
	"gavel_tools_python":     "../../gavel_tools_python+*/lint/aspects/defs.bzl",
	"gavel_tools_typescript": "../../gavel_tools_typescript+*/lint/aspects/defs.bzl",
	"gavel_tools_rust":       "../../gavel_tools_rust+*/lint/aspects/defs.bzl",
	"gavel_tools_rust_rs":    "../../gavel_tools_rust_rs+*/lint/aspects/defs.bzl",
}

// moduleFiles is the same for each module's MODULE.bazel (the core's is the
// repository root, one level above this package).
var moduleFiles = map[string]string{
	coreModule:               "../MODULE.bazel",
	"gavel_tools_go":         "../../gavel_tools_go+*/MODULE.bazel",
	"gavel_tools_java":       "../../gavel_tools_java+*/MODULE.bazel",
	"gavel_tools_python":     "../../gavel_tools_python+*/MODULE.bazel",
	"gavel_tools_typescript": "../../gavel_tools_typescript+*/MODULE.bazel",
	"gavel_tools_rust":       "../../gavel_tools_rust+*/MODULE.bazel",
	"gavel_tools_rust_rs":    "../../gavel_tools_rust_rs+*/MODULE.bazel",
}

func globOne(t *testing.T, pattern string) string {
	t.Helper()
	matches, err := filepath.Glob(pattern)
	require.NoError(t, err)
	require.Len(t, matches, 1, "%s not found exactly once in runfiles", pattern)
	return matches[0]
}

func moduleOf(name string) string {
	if name == "" {
		return coreModule
	}
	return name
}

// exportedAspectsIn extracts the public aspect names a defs.bzl re-binds, which
// is the actual set a consumer can invoke.
func exportedAspectsIn(t *testing.T, path string) []string {
	t.Helper()
	defs, err := os.ReadFile(path)
	require.NoError(t, err)
	re := regexp.MustCompile(`(?m)^([a-z_]+_submission_aspect) = `)
	var exported []string
	for _, match := range re.FindAllStringSubmatch(string(defs), -1) {
		exported = append(exported, match[1])
	}
	return exported
}

// referencedAspects groups every aspect the catalog names (tools and variants)
// by the module that publishes it.
func referencedAspects(catalog catalogFile) map[string][]string {
	byModule := map[string][]string{}
	for _, tools := range catalog.Languages {
		for _, tool := range tools {
			byModule[moduleOf(tool.Module)] = append(byModule[moduleOf(tool.Module)], tool.Aspect)
			for _, v := range tool.Variants {
				byModule[v.Module] = append(byModule[v.Module], v.Aspect)
			}
		}
	}
	return byModule
}

func TestCatalog_EveryModuleExportsExactlyTheAspectsTheCatalogAssignsToIt(t *testing.T) {
	catalog := loadCatalog(t)
	byModule := referencedAspects(catalog)

	for module := range byModule {
		_, known := moduleDefs[module]
		require.True(t, known, "catalog names module %q, which is not wired into this test", module)
	}
	for module, pattern := range moduleDefs {
		path := pattern
		if module != coreModule {
			path = globOne(t, pattern)
		}
		exported := exportedAspectsIn(t, path)
		referenced := byModule[module]
		sort.Strings(exported)
		sort.Strings(referenced)
		assert.Equal(t, exported, referenced,
			"module %s: its defs.bzl must export exactly the aspects the catalog assigns to it — no drift", module)
	}
}

func TestCatalog_BinaryReposAreDeclaredByTheToolsModuleOnly(t *testing.T) {
	catalog := loadCatalog(t)
	declares := func(module, binary string) bool {
		path := moduleFiles[module]
		if module != coreModule {
			path = globOne(t, path)
		}
		text, err := os.ReadFile(path)
		require.NoError(t, err)
		return regexp.MustCompile(`use_repo_rule\([^)]*"` + regexp.QuoteMeta(binary) + `"\)`).Match(text)
	}
	for language, tools := range catalog.Languages {
		for _, tool := range tools {
			if tool.Binary == "" {
				continue
			}
			id := language + "/" + tool.Name
			module := moduleOf(tool.Module)
			assert.True(t, declares(module, tool.Binary),
				"%s: binary %s must be declared in module %s", id, tool.Binary, module)
			for other := range moduleFiles {
				if other != module {
					assert.False(t, declares(other, tool.Binary),
						"%s: binary %s must not be declared in module %s", id, tool.Binary, other)
				}
			}
		}
	}
}

// languageRulesets are the rulesets (and their toolchain providers) a consumer
// chooses; gavel-tools must never make a module graph carry one the consumer
// does not use.
var languageRulesets = []string{
	"rules_go", "rules_java", "rules_python", "rules_rust", "rules_rs",
	"aspect_rules_js", "aspect_rules_ts", "aspect_rules_esbuild", "rules_nodejs", "aspect_bazel_lib",
}

// nonDevBazelDeps returns the bazel_dep names that reach a consumer's module
// graph: a dev_dependency (literally True or the IS_RELEASE switch) does not.
func nonDevBazelDeps(t *testing.T, path string) []string {
	t.Helper()
	text, err := os.ReadFile(path)
	require.NoError(t, err)
	var deps []string
	for _, m := range regexp.MustCompile(`(?m)^bazel_dep\(([^)]*)\)`).FindAllStringSubmatch(string(text), -1) {
		if strings.Contains(m[1], "dev_dependency") {
			continue
		}
		name := regexp.MustCompile(`name = "([^"]+)"`).FindStringSubmatch(m[1])
		require.NotNil(t, name, "bazel_dep without a name in %s", path)
		deps = append(deps, name[1])
	}
	sort.Strings(deps)
	return deps
}

func TestModules_CoreImposesNoLanguageRulesetAndEachModuleOnlyItsOwn(t *testing.T) {
	// Allowed non-dev bazel_deps per module: exactly its own ruleset(s).
	allowed := map[string][]string{
		coreModule:               {},
		"gavel_tools_go":         {"rules_go"},
		"gavel_tools_java":       {"rules_java"},
		"gavel_tools_python":     {"rules_python"},
		"gavel_tools_typescript": {"aspect_rules_js"},
		"gavel_tools_rust":       {"rules_rust"},
		"gavel_tools_rust_rs":    {"rules_rs"},
	}
	for module, own := range allowed {
		path := moduleFiles[module]
		if module != coreModule {
			path = globOne(t, path)
		}
		for _, dep := range nonDevBazelDeps(t, path) {
			if !contains(languageRulesets, dep) {
				continue
			}
			assert.True(t, contains(own, dep),
				"module %s must not carry %s: it is a ruleset gavel-tools may not impose on this module's consumers", module, dep)
		}
		for _, dep := range own {
			assert.True(t, contains(nonDevBazelDeps(t, path), dep),
				"module %s must depend on its own ruleset %s", module, dep)
		}
	}
}

// All modules (including gavel_tools_web, which the catalog does not name) are
// released from one tag, so they share one version and depend on each other at
// exactly that version.
func TestModules_AllShareOneVersionAndPinEachOtherToIt(t *testing.T) {
	files := map[string]string{"gavel_tools_web": "../../gavel_tools_web+*/MODULE.bazel"}
	for module, pattern := range moduleFiles {
		files[module] = pattern
	}
	moduleVersion := regexp.MustCompile(`(?s)module\(\s*name = "([^"]+)",\s*version = "([^"]+)"`)
	depVersion := regexp.MustCompile(`(?m)^bazel_dep\(name = "(gavel_tools[a-z_]*)", version = "([^"]+)"`)

	versions := map[string]bool{}
	for module, pattern := range files {
		path := "../MODULE.bazel"
		if module != coreModule {
			path = globOne(t, pattern)
		}
		text, err := os.ReadFile(path)
		require.NoError(t, err)
		m := moduleVersion.FindStringSubmatch(string(text))
		require.NotNil(t, m, "no module() version in %s", path)
		assert.Equal(t, module, m[1])
		versions[m[2]] = true
		for _, dep := range depVersion.FindAllStringSubmatch(string(text), -1) {
			versions[dep[2]] = true
		}
	}
	assert.Len(t, versions, 1, "every module and every gavel_tools* bazel_dep must carry the same version, got %v", versions)
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

func TestCatalog_ClippyCapturesOutputInsteadOfFailingTheBuild(t *testing.T) {
	catalog := loadCatalog(t)

	var clippy toolEntry
	for _, tool := range catalog.Languages["rust"] {
		if tool.Name == "clippy" {
			clippy = tool
		}
	}
	require.Equal(t, "clippy", clippy.Name, "rust/clippy must exist in the catalog")

	assert.Contains(t, clippy.BuildFlags, "--@rules_rust//rust/settings:capture_clippy_output=True",
		"without capture, rules_rust runs Clippy as a check that fails the build on findings")
	assert.Contains(t, clippy.BuildFlags, "--@rules_rust//rust/settings:clippy_output_diagnostics=True",
		"the submission aspect reads the captured diagnostics to produce SARIF")
}

func TestCatalog_GoStdlibFlagUsesCanonicalRulesGoName(t *testing.T) {
	catalog := loadCatalog(t)

	var golangci toolEntry
	for _, tool := range catalog.Languages["go"] {
		if tool.Name == "golangci-lint" {
			golangci = tool
		}
	}
	require.Equal(t, "golangci-lint", golangci.Name, "go/golangci-lint must exist in the catalog")

	require.Len(t, golangci.BuildFlags, 1)
	assert.True(t, strings.HasPrefix(golangci.BuildFlags[0], "--@@rules_go+//"),
		"export_stdlib must use rules_go's canonical repo name (@@rules_go+) so it resolves on consumers that alias rules_go under a different apparent name")
}

func TestCatalog_EntriesAreWellFormed(t *testing.T) {
	catalog := loadCatalog(t)

	assert.Equal(t, 1, catalog.Version)
	assert.NotEmpty(t, catalog.AspectsBzl)

	seen := map[string]bool{}
	for language, tools := range catalog.Languages {
		require.NotEmpty(t, tools, "language %q lists no tools", language)
		for _, tool := range tools {
			assert.NotEmpty(t, tool.Name, "a tool in %q has no name", language)
			assert.NotEmpty(t, tool.Aspect, "%s/%s has no aspect", language, tool.Name)
			assert.True(t, strings.HasPrefix(tool.SARIFSuffix, "."),
				"%s/%s sarif_suffix must start with a dot", language, tool.Name)

			key := language + "/" + tool.Name
			assert.False(t, seen[key], "duplicate tool %q", key)
			seen[key] = true
		}
	}
}

func TestCatalog_VariantsAreWellFormed(t *testing.T) {
	catalog := loadCatalog(t)
	count := 0
	for language, tools := range catalog.Languages {
		for _, tool := range tools {
			for _, v := range tool.Variants {
				count++
				id := language + "/" + tool.Name
				assert.NotEmpty(t, v.WhenBazelDep, "%s variant has no when_bazel_dep", id)
				assert.NotEmpty(t, v.Module, "%s variant has no module", id)
				assert.NotEmpty(t, v.AspectsBzl, "%s variant has no aspects_bzl", id)
				assert.NotEmpty(t, v.Aspect, "%s variant has no aspect", id)
			}
		}
	}
	assert.NotZero(t, count, "the catalog must publish at least the rules_rs clippy variant")
}

func TestCatalog_RulesRsClippyVariantBindsToTheRulesRsModule(t *testing.T) {
	catalog := loadCatalog(t)
	var clippy toolEntry
	for _, tool := range catalog.Languages["rust"] {
		if tool.Name == "clippy" {
			clippy = tool
		}
	}
	assert.Equal(t, "gavel_tools_rust", clippy.Module)
	require.Len(t, clippy.Variants, 1)
	v := clippy.Variants[0]
	assert.Equal(t, "rules_rs", v.WhenBazelDep)
	assert.Equal(t, "gavel_tools_rust_rs", v.Module)
	assert.Equal(t, "//lint/aspects:defs.bzl", v.AspectsBzl)
	assert.Equal(t, "rust_clippy_submission_aspect", v.Aspect)
}
