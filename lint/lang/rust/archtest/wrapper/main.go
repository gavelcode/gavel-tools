package wrapper

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gavelcode/gavel-tools/lint/archtest"
	"github.com/gavelcode/gavel-tools/lint/sarif"
)

const (
	exitCodeUsageError = 2
	dirPermission      = 0o755
)

// Main runs the rust-archtest tool with args (the tool name already stripped) and
// returns the process exit code.
func Main(arguments []string) int {
	flags := flag.NewFlagSet("rust-archtest", flag.ExitOnError)
	config := flags.String("config", "", "Path to architecture.yml")
	outputPath := flags.String("out", "", "SARIF output path")
	packageName := flags.String("package", "", "Bazel package of the crate being checked")
	crates := crateFlag{}
	flags.Var(crates, "crate", "Direct dependency as crate_name=package (repeatable)")
	_ = flags.Parse(arguments)

	if *config == "" {
		fmt.Fprintln(os.Stderr, "missing --config")
		return exitCodeUsageError
	}
	if *outputPath == "" {
		fmt.Fprintln(os.Stderr, "missing --out")
		return exitCodeUsageError
	}

	files := flags.Args()
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "missing Rust source files")
		return exitCodeUsageError
	}

	if err := run(*config, *outputPath, files, resolver{pkg: *packageName, crates: crates}); err != nil {
		fmt.Fprintf(os.Stderr, "run rust archtest: %v\n", err)
		return 1
	}
	return 0
}

func run(configPath, outputPath string, files []string, rustResolver resolver) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), dirPermission); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	config, err := archtest.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	var allViolations []archtest.Violation
	var failures []string
	for _, sourceFile := range files {
		violations, err := evaluateFile(sourceFile, config, rustResolver)
		if err != nil {
			failures = append(failures, fmt.Sprintf("could not analyze %s: %v", sourceFile, err))
			continue
		}
		allViolations = append(allViolations, violations...)
	}

	invocation := sarif.Successful()
	if len(failures) > 0 {
		invocation = sarif.Failed(failures...)
	}
	if err := archtest.WriteSARIFWithInvocation(outputPath, "rust_archtest", allViolations, invocation); err != nil {
		return fmt.Errorf("write sarif: %w", err)
	}
	return nil
}

func evaluateFile(sourceFile string, config archtest.Config, rustResolver resolver) ([]archtest.Violation, error) {
	pkgDir := filepath.Dir(sourceFile)
	layer := archtest.MatchLayer(pkgDir, config.Layers)
	if layer == "" {
		return nil, nil
	}

	imports, err := parseRustImports(sourceFile, rustResolver)
	if err != nil {
		return nil, err
	}

	return archtest.Evaluate(sourceFile, layer, dedupeImports(imports), config.Layers, config.Rules), nil
}

// dedupeImports drops repeated (path, line) pairs, which a brace group such as
// `model::{A, B}` produces, so each is reported once.
func dedupeImports(imports []archtest.Import) []archtest.Import {
	alreadySeen := make(map[archtest.Import]bool, len(imports))
	unique := make([]archtest.Import, 0, len(imports))
	for _, imp := range imports {
		if alreadySeen[imp] {
			continue
		}
		alreadySeen[imp] = true
		unique = append(unique, imp)
	}
	return unique
}

func parseRustImports(filePath string, rustResolver resolver) (imports []archtest.Import, retErr error) {
	sourceFile, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", filePath, err)
	}
	defer func() {
		if closeErr := sourceFile.Close(); closeErr != nil && retErr == nil {
			retErr = fmt.Errorf("close %s: %w", filePath, closeErr)
		}
	}()

	collector := useCollector{fileDir: filepath.Dir(filePath), resolver: rustResolver}
	scanner := bufio.NewScanner(sourceFile)
	lineNum := 0
	inBlockComment := false
	for scanner.Scan() {
		lineNum++
		trimmed, skip := cleanRustCommentLine(strings.TrimSpace(scanner.Text()), &inBlockComment)
		if skip {
			continue
		}
		collector.consume(strings.TrimSpace(stripRustLineComment(trimmed)), lineNum)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", filePath, err)
	}
	return collector.imports, nil
}

// useCollector accumulates the imports of one file. A use statement may span
// lines (rustfmt wraps long brace groups), so code is buffered from the `use`
// keyword until the terminating `;`.
type useCollector struct {
	fileDir     string
	resolver    resolver
	pending     strings.Builder
	pendingLine int
	imports     []archtest.Import
}

// consume feeds one comment-free line of code, which may hold the start, the
// end, or several whole use statements.
func (c *useCollector) consume(source string, lineNum int) {
	for source != "" {
		if c.pending.Len() == 0 {
			rest, ok := cutUseKeyword(source)
			if !ok {
				return
			}
			c.pendingLine = lineNum
			source = rest
		}
		semicolon := strings.Index(source, ";")
		if semicolon < 0 {
			c.pending.WriteString(source)
			c.pending.WriteString(" ")
			return
		}
		c.pending.WriteString(source[:semicolon])
		c.imports = append(c.imports, parseUseStatement(c.pending.String(), c.fileDir, c.pendingLine, c.resolver)...)
		c.pending.Reset()
		source = strings.TrimSpace(source[semicolon+1:])
	}
}

// cutUseKeyword returns the text after a leading `use`, `pub use`,
// `pub(crate) use`, `pub(super) use` or `pub(in path) use`.
func cutUseKeyword(source string) (string, bool) {
	if visibility, ok := strings.CutPrefix(source, "pub"); ok {
		visibility = strings.TrimLeft(visibility, " \t")
		if strings.HasPrefix(visibility, "(") {
			closing := strings.Index(visibility, ")")
			if closing < 0 {
				return "", false
			}
			visibility = strings.TrimLeft(visibility[closing+1:], " \t")
		}
		source = visibility
	}
	rest, ok := strings.CutPrefix(source, "use")
	if !ok || rest == "" || (rest[0] != ' ' && rest[0] != '\t' && rest[0] != ':' && rest[0] != '{') {
		return "", false
	}
	return strings.TrimSpace(rest), true
}

// stripRustLineComment drops a trailing `//` comment.
func stripRustLineComment(code string) string {
	if idx := strings.Index(code, "//"); idx >= 0 {
		return code[:idx]
	}
	return code
}

// cleanRustCommentLine advances the block-comment state and strips comments,
// returning the code left on the line and whether the caller should skip it.
func cleanRustCommentLine(trimmed string, inBlockComment *bool) (string, bool) {
	if *inBlockComment {
		idx := strings.Index(trimmed, "*/")
		if idx < 0 {
			return "", true
		}
		*inBlockComment = false
		trimmed = strings.TrimSpace(trimmed[idx+2:])
		if trimmed == "" {
			return "", true
		}
	}
	trimmed = stripRustBlockComments(trimmed)
	if strings.HasPrefix(trimmed, "//") {
		return "", true
	}
	if strings.Contains(trimmed, "/*") && !strings.Contains(trimmed, "*/") {
		*inBlockComment = true
		trimmed = strings.TrimSpace(trimmed[:strings.Index(trimmed, "/*")])
		if trimmed == "" {
			return "", true
		}
	}
	return trimmed, false
}

// parseUseStatement turns the text of one use statement (without `use` and
// `;`) into one import per leaf path of its brace tree.
func parseUseStatement(statement, fileDir string, lineNum int, rustResolver resolver) []archtest.Import {
	var imports []archtest.Import
	for _, leaf := range expandUseTree("", statement) {
		if isStdlibUse(leaf) {
			continue
		}
		filePath := rustResolver.resolve(leaf, fileDir)
		if filePath == "" {
			continue
		}
		imports = append(imports, archtest.Import{Path: filePath, Line: lineNum})
	}
	return imports
}

// expandUseTree flattens a use tree (`a::{b::{C, D}, e as f, g::*}`) into the
// full `::` paths of its leaves, dropping aliases and glob markers. `self`
// stands for the group's own prefix and is only reported when the group has no
// other leaf, since any sibling already pins the same module.
func expandUseTree(prefix, tree string) []string {
	var leaves []string
	sawSelf := false
	for _, treeItem := range splitTopLevel(tree) {
		itemLeaves, isSelf := expandUseItem(prefix, stripLeadingColons(strings.TrimSpace(treeItem)))
		sawSelf = sawSelf || isSelf
		leaves = append(leaves, itemLeaves...)
	}
	if sawSelf && len(leaves) == 0 && prefix != "" {
		leaves = append(leaves, prefix)
	}
	return leaves
}

// expandUseItem expands one comma-separated element of a use tree. It reports
// isSelf for a bare `self`, which the caller resolves against its siblings.
func expandUseItem(prefix, treeItem string) (leaves []string, isSelf bool) {
	if treeItem == "" {
		return nil, false
	}
	if open := strings.Index(treeItem, "{"); open >= 0 {
		group := strings.TrimSuffix(strings.TrimSpace(treeItem[:open]), "::")
		inner := strings.TrimSuffix(strings.TrimSpace(treeItem[open+1:]), "}")
		return expandUseTree(joinPath(prefix, group), inner), false
	}
	if idx := strings.Index(treeItem, " as "); idx >= 0 {
		treeItem = strings.TrimSpace(treeItem[:idx])
	}
	treeItem = strings.TrimSuffix(treeItem, "::*")
	switch treeItem {
	case "self":
		return nil, true
	case "*":
		return []string{prefix}, false
	default:
		return []string{joinPath(prefix, treeItem)}, false
	}
}

func joinPath(prefix, suffix string) string {
	if prefix == "" {
		return suffix
	}
	if suffix == "" {
		return prefix
	}
	return prefix + "::" + suffix
}

// splitTopLevel splits on commas that are not inside braces.
func splitTopLevel(useText string) []string {
	var parts []string
	depth, start := 0, 0
	for offset, char := range useText {
		switch char {
		case '{':
			depth++
		case '}':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, useText[start:offset])
				start = offset + 1
			}
		}
	}
	return append(parts, useText[start:])
}

// resolve maps a use path to a workspace-relative path, or "" when it cannot
// be placed (third-party crates, or crate:: with no known package).
func (r resolver) resolve(statement, fileDir string) string {
	statement = strings.Join(strings.Fields(stripLeadingColons(strings.TrimSpace(statement))), "")
	firstSegment, modulePath, _ := strings.Cut(statement, "::")

	switch firstSegment {
	case "crate":
		return joinSlash(r.pkg, "src", moduleDir(modulePath))
	case "self":
		return joinSlash(fileDir, moduleDir(modulePath))
	case "super":
		return resolveSuper(fileDir, modulePath)
	}

	if crateDir, ok := r.crates[firstSegment]; ok {
		return joinSlash(crateDir, "src", moduleDir(modulePath))
	}
	return ""
}

// resolveSuper walks one directory up for the leading `super` and again for
// each further `super::` in modulePath.
func resolveSuper(fileDir, modulePath string) string {
	currentDir := filepath.Dir(fileDir)
	for {
		remaining, ok := strings.CutPrefix(modulePath, "super::")
		if modulePath == "super" {
			remaining, ok = "", true
		}
		if !ok {
			break
		}
		currentDir = filepath.Dir(currentDir)
		modulePath = remaining
	}
	return joinSlash(currentDir, moduleDir(modulePath))
}

func moduleDir(modPath string) string {
	return strings.ReplaceAll(trimToModule(modPath), "::", "/")
}

func joinSlash(parts ...string) string {
	return filepath.ToSlash(filepath.Join(parts...))
}

func trimToModule(filePath string) string {
	parts := strings.Split(filePath, "::")

	last := parts[len(parts)-1]
	if len(last) > 0 && last[0] >= 'A' && last[0] <= 'Z' {
		// A lone type sits at the module root, so the module path is empty.
		return strings.Join(parts[:len(parts)-1], "::")
	}

	return filePath
}

func isStdlibUse(statement string) bool {
	return strings.HasPrefix(statement, "std::") ||
		strings.HasPrefix(statement, "core::") ||
		strings.HasPrefix(statement, "alloc::")
}

func stripRustBlockComments(lineText string) string {
	for {
		start := strings.Index(lineText, "/*")
		if start < 0 {
			return lineText
		}
		end := strings.Index(lineText[start+2:], "*/")
		if end < 0 {
			return lineText
		}
		lineText = lineText[:start] + lineText[start+2+end+2:]
	}
}

func stripLeadingColons(segment string) string {
	for strings.HasPrefix(segment, "::") {
		segment = segment[2:]
	}
	return segment
}

type resolver struct {
	pkg    string
	crates map[string]string
}

// crateFlag collects repeated --crate name=package flags.
type crateFlag map[string]string

func (f crateFlag) String() string {
	pairs := make([]string, 0, len(f))
	for name, pkg := range f {
		pairs = append(pairs, name+"="+pkg)
	}
	sort.Strings(pairs)
	return strings.Join(pairs, ",")
}

func (f crateFlag) Set(value string) error {
	name, pkg, ok := strings.Cut(value, "=")
	if !ok || name == "" {
		return fmt.Errorf("invalid --crate %q, want name=package (package may be empty for the workspace root)", value)
	}
	f[name] = pkg
	return nil
}
