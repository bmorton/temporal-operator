// Command migration-runner is a lightweight Temporal worker/client used by the
// migration-proxy e2e suite. The EchoWorkflow runs WhoAmIActivity, which each
// worker registers to return its own cluster label, so the client can prove
// which cluster executed a workflow.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
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

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: migration-runner <worker|client> [flags]")
	}
	switch os.Args[1] {
	case "worker":
		runWorker(os.Args[2:])
	case "client":
		runClient(os.Args[2:])
	default:
		log.Fatalf("unknown subcommand %q (want worker|client)", os.Args[1])
	}
}

// runWorker connects directly to a cluster frontend and serves EchoWorkflow,
// registering a WhoAmIActivity that returns this worker's cluster label.
func runWorker(args []string) {
	fs := flag.NewFlagSet("worker", flag.ExitOnError)
	address := fs.String("address", "", "frontend address host:port")
	namespace := fs.String("namespace", "migration", "Temporal namespace")
	cluster := fs.String("cluster", "", "cluster label this worker reports")
	_ = fs.Parse(args)
	if *address == "" || *cluster == "" {
		log.Fatal("worker: --address and --cluster are required")
	}

	c := mustDial(*address, *namespace)
	defer c.Close()

	w := worker.New(c, taskQueue, worker.Options{})
	w.RegisterWorkflow(EchoWorkflow)
	label := *cluster
	w.RegisterActivityWithOptions(
		func(ctx context.Context) (string, error) { return label, nil },
		activity.RegisterOptions{Name: "WhoAmIActivity"},
	)
	log.Printf("worker starting: cluster=%s address=%s namespace=%s", *cluster, *address, *namespace)
	if err := w.Run(worker.InterruptCh()); err != nil {
		log.Fatalf("worker exited: %v", err)
	}
}

// runClient starts EchoWorkflow through the proxy endpoint, waits for the
// result, and asserts it matches --expect. It exits non-zero on mismatch or
// timeout so a Kubernetes Job records the outcome.
func runClient(args []string) {
	fs := flag.NewFlagSet("client", flag.ExitOnError)
	address := fs.String("address", "", "proxy endpoint host:port")
	namespace := fs.String("namespace", "migration", "Temporal namespace")
	workflowID := fs.String("workflow-id", "", "workflow id")
	expect := fs.String("expect", "", "expected cluster label (source|target)")
	timeout := fs.Duration("timeout", 2*time.Minute, "overall timeout")
	_ = fs.Parse(args)
	if *address == "" || *workflowID == "" || *expect == "" {
		log.Fatal("client: --address, --workflow-id and --expect are required")
	}

	c := mustDial(*address, *namespace)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	run, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        *workflowID,
		TaskQueue: taskQueue,
	}, EchoWorkflow)
	if err != nil {
		log.Fatalf("start workflow: %v", err)
	}
	log.Printf("started workflow id=%s run=%s", run.GetID(), run.GetRunID())

	var got string
	if err := run.Get(ctx, &got); err != nil {
		log.Fatalf("get result: %v", err)
	}
	if got != *expect {
		log.Fatalf("FAIL: workflow ran on %q, expected %q", got, *expect)
	}
	log.Printf("OK: workflow ran on %q as expected", got)
}

// mustDial retries dialing the frontend until success or a deadline, tolerating
// frontends that are still warming up when the pod starts.
func mustDial(address, namespace string) client.Client {
	deadline := time.Now().Add(3 * time.Minute)
	for {
		c, err := client.Dial(client.Options{HostPort: address, Namespace: namespace})
		if err == nil {
			return c
		}
		if time.Now().After(deadline) {
			log.Fatalf("dial %s (namespace %s): %v", address, namespace, err)
		}
		log.Printf("dial %s failed, retrying: %v", address, err)
		time.Sleep(3 * time.Second)
	}
}
