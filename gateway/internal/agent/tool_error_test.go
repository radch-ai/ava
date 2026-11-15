package agent

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCleanToolErrorPeelsInvokeHarnessFrame(t *testing.T) {
	harnessed := fmt.Errorf(
		"[LocalFunc] failed to invoke tool, toolName=echo_tool, err=%w",
		errors.New("search backend down"))
	require.Equal(t, "search backend down",
		cleanToolError(&toolError{name: "echo_tool", err: harnessed}))
}

func TestCleanToolErrorKeepsPlainToolError(t *testing.T) {
	plain := errors.New("search backend down")
	require.Equal(t, "search backend down",
		cleanToolError(&toolError{name: "echo_tool", err: plain}))
}

func TestToolFailureNamesToolAndCause(t *testing.T) {
	plain := errors.New("search backend down")
	tool, cause, ok := toolFailure(&toolError{name: "echo_tool", err: plain})
	require.True(t, ok)
	require.Equal(t, "echo_tool", tool)
	require.Equal(t, "search backend down", cause)
}

func TestToolFailureRejectsOtherErrors(t *testing.T) {
	_, _, ok := toolFailure(errors.New("read default model: no rows"))
	require.False(t, ok)
}
