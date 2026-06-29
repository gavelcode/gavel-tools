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

func TestFailed_NoNotes(t *testing.T) {
	inv := Failed()

	assert.False(t, inv.ExecutionSuccessful)
	assert.Empty(t, inv.ToolExecutionNotifications)
}
