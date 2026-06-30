package archtest

import (
	"fmt"
	"strings"
)

type Import struct {
	Path string
	Line int
}

type Violation struct {
	RuleName   string
	SourceFile string
	SourceLine int
	SourcePkg  string
	TargetPkg  string
	Message    string
}

func EvaluateWithModule(sourceFile, sourceLayer string, imports []Import, layers map[string][]string, rules []Rule, modulePrefix string) []Violation {
	if modulePrefix != "" {
		stripped := make([]Import, 0, len(imports))
		prefix := modulePrefix + "/"
		for _, importEntry := range imports {
			if strings.HasPrefix(importEntry.Path, prefix) {
				stripped = append(stripped, Import{Path: strings.TrimPrefix(importEntry.Path, prefix), Line: importEntry.Line})
			} else {
				stripped = append(stripped, importEntry)
			}
		}
		imports = stripped
	}
	return Evaluate(sourceFile, sourceLayer, imports, layers, rules)
}

func Evaluate(sourceFile, sourceLayer string, imports []Import, layers map[string][]string, rules []Rule) []Violation {
	applicableRules := filterRulesForLayer(sourceLayer, rules)
	if len(applicableRules) == 0 {
		return nil
	}

	var violations []Violation
	for _, importEntry := range imports {
		targetLayer := MatchLayer(importEntry.Path, layers)
		if targetLayer == "" || targetLayer == sourceLayer {
			continue
		}

		for _, archRule := range applicableRules {
			if isDenied(targetLayer, archRule.Deny) {
				violations = append(violations, Violation{
					RuleName:   archRule.Name,
					SourceFile: sourceFile,
					SourceLine: importEntry.Line,
					SourcePkg:  sourceLayer,
					TargetPkg:  targetLayer,
					Message:    fmt.Sprintf("%s imports %s (layer %q -> %q denied by rule %q)", sourceFile, importEntry.Path, sourceLayer, targetLayer, archRule.Name),
				})
			}
		}
	}

	return violations
}

func filterRulesForLayer(layer string, rules []Rule) []Rule {
	var matched []Rule
	for _, r := range rules {
		if r.Source == layer {
			matched = append(matched, r)
		}
	}
	return matched
}

func isDenied(targetLayer string, denyList []string) bool {
	for _, denied := range denyList {
		if denied == targetLayer {
			return true
		}
	}
	return false
}
