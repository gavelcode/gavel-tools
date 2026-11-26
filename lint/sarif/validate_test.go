package sarif

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validDoc = `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"todo"}},"results":[
 {"ruleId":"todo/found","level":"warning","message":{"text":"TODO found"},
  "locations":[{"physicalLocation":{"artifactLocation":{"uri":"pkg/a.txt"},"region":{"startLine":3}}}]}],
 "invocations":[{"executionSuccessful":true}]}]}`

func TestValidateBridge_AcceptsValid(t *testing.T) {
	assert.Empty(t, ValidateBridge([]byte(validDoc)))
}

func TestValidateBridge_AcceptsFailedRunWithNotification(t *testing.T) {
	doc := `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"t"}},"results":[],
 "invocations":[{"executionSuccessful":false,"toolExecutionNotifications":[{"level":"error","message":{"text":"boom"}}]}]}]}`
	assert.Empty(t, ValidateBridge([]byte(doc)))
}

func TestValidateBridge_AcceptsMissingLevel(t *testing.T) {
	doc := strings.Replace(validDoc, `"level":"warning",`, "", 1)
	assert.Empty(t, ValidateBridge([]byte(doc)))
}

func TestValidateBridge_Rejects(t *testing.T) {
	cases := map[string]struct{ from, to, want string }{
		"empty ruleId":     {`"ruleId":"todo/found"`, `"ruleId":"  "`, "ruleId"},
		"missing ruleId":   {`"ruleId":"todo/found",`, ``, "ruleId"},
		"empty message":    {`"text":"TODO found"`, `"text":""`, "message.text"},
		"no locations":     {`"locations":[{"physicalLocation":{"artifactLocation":{"uri":"pkg/a.txt"},"region":{"startLine":3}}}]`, `"locations":[]`, "locations"},
		"empty uri":        {`"uri":"pkg/a.txt"`, `"uri":""`, "uri"},
		"absolute uri":     {`"uri":"pkg/a.txt"`, `"uri":"/abs/a.txt"`, "workspace-relative"},
		"file uri":         {`"uri":"pkg/a.txt"`, `"uri":"file:///a.txt"`, "workspace-relative"},
		"execroot uri":     {`"uri":"pkg/a.txt"`, `"uri":"bazel-out/execroot/_main/a.txt"`, "workspace-relative"},
		"parent uri":       {`"uri":"pkg/a.txt"`, `"uri":"../a.txt"`, "workspace-relative"},
		"zero line":        {`"startLine":3`, `"startLine":0`, "startLine"},
		"unknown level":    {`"level":"warning"`, `"level":"fatal"`, "level"},
		"no tool name":     {`"name":"todo"`, `"name":""`, "tool.driver.name"},
		"failed w/o notes": {`"executionSuccessful":true`, `"executionSuccessful":false`, "executionSuccessful"},
		"no runs":          {`"runs":[`, `"runs":[],"x":[`, "runs"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			require.Contains(t, validDoc, c.from)
			problems := ValidateBridge([]byte(strings.Replace(validDoc, c.from, c.to, 1)))
			require.NotEmpty(t, problems)
			assert.Contains(t, strings.Join(problems, "\n"), c.want)
		})
	}
}

func TestValidateBridge_RejectsInvalidJSON(t *testing.T) {
	assert.NotEmpty(t, ValidateBridge([]byte("{")))
}
