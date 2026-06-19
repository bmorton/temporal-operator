package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func TestEchoWorkflowReturnsActivityLabel(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(EchoWorkflow)
	env.RegisterActivityWithOptions(
		func(ctx context.Context) (string, error) { return "target", nil },
		activity.RegisterOptions{Name: "WhoAmIActivity"},
	)

	env.ExecuteWorkflow(EchoWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var result string
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, "target", result)
}
