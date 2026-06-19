// Command migration-runner is a lightweight Temporal worker/client used by the
// migration-proxy e2e suite. The EchoWorkflow runs WhoAmIActivity, which each
// worker registers to return its own cluster label, so the client can prove
// which cluster executed a workflow.
package main

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

// taskQueue is shared by the source worker, the target worker, and the client.
const taskQueue = "migration-e2e"

// EchoWorkflow executes WhoAmIActivity and returns the executing worker's
// cluster label.
func EchoWorkflow(ctx workflow.Context) (string, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
	})
	var who string
	if err := workflow.ExecuteActivity(ctx, "WhoAmIActivity").Get(ctx, &who); err != nil {
		return "", err
	}
	return who, nil
}

func main() {}
