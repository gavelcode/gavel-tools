package sarif

import (
	"encoding/json"
	"fmt"
	"strings"
)

// bridgeDocument is the subset of SARIF the bridge contract constrains.
type bridgeDocument struct {
	Runs []bridgeRun `json:"runs"`
}

type bridgeRun struct {
	Tool struct {
		Driver struct {
			Name string `json:"name"`
		} `json:"driver"`
	} `json:"tool"`
	Results     []bridgeResult     `json:"results"`
	Invocations []bridgeInvocation `json:"invocations"`
}

type bridgeResult struct {
	RuleID  string `json:"ruleId"`
	Level   string `json:"level"`
	Message struct {
		Text string `json:"text"`
	} `json:"message"`
	Locations []struct {
		PhysicalLocation struct {
			ArtifactLocation struct {
				URI string `json:"uri"`
			} `json:"artifactLocation"`
			Region struct {
				StartLine int `json:"startLine"`
			} `json:"region"`
		} `json:"physicalLocation"`
	} `json:"locations"`
}

type bridgeInvocation struct {
	ExecutionSuccessful        bool `json:"executionSuccessful"`
	ToolExecutionNotifications []struct {
		Message struct {
			Text string `json:"text"`
		} `json:"message"`
	} `json:"toolExecutionNotifications"`
}

// ValidateBridge checks data against the bridge SARIF contract documented in
// docs/bridge.md and returns one human-readable problem per violation (empty
// when the document conforms). It is deliberately stricter than gavel's
// ingestion parser, which tolerates missing locations; a bridge author should
// not rely on that tolerance.
func ValidateBridge(data []byte) []string {
	var document bridgeDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return []string{fmt.Sprintf("not valid SARIF JSON: %v", err)}
	}
	if len(document.Runs) == 0 {
		return []string{"runs: at least one run is required"}
	}
	var problems []string
	for runIndex, run := range document.Runs {
		for _, problem := range validateRun(run) {
			problems = append(problems, fmt.Sprintf("runs[%d]: %s", runIndex, problem))
		}
	}
	return problems
}

func validateRun(run bridgeRun) []string {
	var problems []string
	if strings.TrimSpace(run.Tool.Driver.Name) == "" {
		problems = append(problems, "tool.driver.name must be non-empty")
	}
	for resultIndex, result := range run.Results {
		for _, problem := range validateResult(result) {
			problems = append(problems, fmt.Sprintf("results[%d]: %s", resultIndex, problem))
		}
	}
	for invocationIndex, invocation := range run.Invocations {
		if problem := validateInvocation(invocation); problem != "" {
			problems = append(problems, fmt.Sprintf("invocations[%d]: %s", invocationIndex, problem))
		}
	}
	return problems
}

func validateResult(result bridgeResult) []string {
	var problems []string
	if strings.TrimSpace(result.RuleID) == "" {
		problems = append(problems, "ruleId must be non-empty")
	}
	if strings.TrimSpace(result.Message.Text) == "" {
		problems = append(problems, "message.text must be non-empty")
	}
	switch result.Level {
	case "", "error", "warning", "note", "none":
	default:
		problems = append(problems, fmt.Sprintf("level %q must be error, warning, note or none", result.Level))
	}
	if len(result.Locations) == 0 {
		return append(problems, "locations[0] is required")
	}
	location := result.Locations[0].PhysicalLocation
	uri := location.ArtifactLocation.URI
	switch {
	case uri == "":
		problems = append(problems, "locations[0].physicalLocation.artifactLocation.uri must be non-empty")
	case !workspaceRelative(uri):
		problems = append(problems, fmt.Sprintf("uri %q must be workspace-relative (no scheme, leading '/', '..' or execroot)", uri))
	}
	if location.Region.StartLine < 1 {
		problems = append(problems, "locations[0].physicalLocation.region.startLine must be >= 1")
	}
	return problems
}

// validateInvocation returns the problem with a failed invocation that does not
// say why it failed, or "" when the invocation is acceptable.
func validateInvocation(invocation bridgeInvocation) string {
	if invocation.ExecutionSuccessful {
		return ""
	}
	for _, notification := range invocation.ToolExecutionNotifications {
		if strings.TrimSpace(notification.Message.Text) != "" {
			return ""
		}
	}
	return "executionSuccessful=false requires a toolExecutionNotifications entry with message.text"
}

func workspaceRelative(uri string) bool {
	if strings.Contains(uri, "://") || strings.HasPrefix(uri, "/") || strings.Contains(uri, "execroot/") {
		return false
	}
	for _, part := range strings.Split(uri, "/") {
		if part == ".." {
			return false
		}
	}
	return true
}
