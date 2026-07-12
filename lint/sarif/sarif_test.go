package sarif

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSuccessful(t *testing.T) {
	inv := Successful()

	assert.True(t, inv.ExecutionSuccessful)
	assert.Empty(t, inv.ToolExecutionNotifications)
}

func TestFailed_CarriesReasons(t *testing.T) {
	inv := Failed("ruff: invalid pyproject.toml", "second reason")

	assert.False(t, inv.ExecutionSuccessful)
	require.Len(t, inv.ToolExecutionNotifications, 2)
	assert.Equal(t, "error", inv.ToolExecutionNotifications[0].Level)
	assert.Equal(t, "ruff: invalid pyproject.toml", inv.ToolExecutionNotifications[0].Message.Text)
}

func TestDegraded_StaysSuccessfulWithWarnings(t *testing.T) {
	inv := Degraded("13 compilation errors prevented complete analysis; results are incomplete")

	assert.True(t, inv.ExecutionSuccessful,
		"a degraded run ran to completion — executionSuccessful must stay true so the consumer does not fail the verdict")
	require.Len(t, inv.ToolExecutionNotifications, 1)
	assert.Equal(t, "warning", inv.ToolExecutionNotifications[0].Level)
	assert.Contains(t, inv.ToolExecutionNotifications[0].Message.Text, "incomplete")
}

func TestFailed_NoNotes(t *testing.T) {
	inv := Failed()

	assert.False(t, inv.ExecutionSuccessful)
	assert.Empty(t, inv.ToolExecutionNotifications)
}
