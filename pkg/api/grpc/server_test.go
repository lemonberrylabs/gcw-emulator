package grpcapi

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	executionspb "cloud.google.com/go/workflows/executions/apiv1/executionspb"
	workflowspb "cloud.google.com/go/workflows/apiv1/workflowspb"

	"github.com/lemonberrylabs/gcw-emulator/pkg/store"
)

func startTestServer(t *testing.T) (string, func()) {
	t.Helper()
	s := store.New()
	srv := New(s)

	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	go srv.grpc.Serve(lis)

	return lis.Addr().String(), func() {
		srv.grpc.Stop()
	}
}

func dial(t *testing.T, addr string) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	return conn
}

func TestCreateAndGetWorkflow(t *testing.T) {
	addr, cleanup := startTestServer(t)
	defer cleanup()

	conn := dial(t, addr)
	defer conn.Close()

	client := workflowspb.NewWorkflowsClient(conn)
	ctx := context.Background()

	// Create
	op, err := client.CreateWorkflow(ctx, &workflowspb.CreateWorkflowRequest{
		Parent:     "projects/my-project/locations/us-central1",
		WorkflowId: "hello",
		Workflow: &workflowspb.Workflow{
			SourceCode: &workflowspb.Workflow_SourceContents{
				SourceContents: "main:\n  steps:\n    - returnHello:\n        return: \"hello\"",
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if !op.GetDone() {
		t.Fatal("expected operation to be done")
	}

	// Get
	wf, err := client.GetWorkflow(ctx, &workflowspb.GetWorkflowRequest{
		Name: "projects/my-project/locations/us-central1/workflows/hello",
	})
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if wf.GetName() != "projects/my-project/locations/us-central1/workflows/hello" {
		t.Fatalf("unexpected name: %s", wf.GetName())
	}
	if wf.GetState() != workflowspb.Workflow_ACTIVE {
		t.Fatalf("unexpected state: %v", wf.GetState())
	}
	if wf.GetSourceContents() == "" {
		t.Fatal("expected source_contents to be set")
	}
}

func TestListWorkflows(t *testing.T) {
	addr, cleanup := startTestServer(t)
	defer cleanup()

	conn := dial(t, addr)
	defer conn.Close()

	client := workflowspb.NewWorkflowsClient(conn)
	ctx := context.Background()

	parent := "projects/my-project/locations/us-central1"

	// Create two workflows
	for _, id := range []string{"wf-a", "wf-b"} {
		_, err := client.CreateWorkflow(ctx, &workflowspb.CreateWorkflowRequest{
			Parent:     parent,
			WorkflowId: id,
			Workflow: &workflowspb.Workflow{
				SourceCode: &workflowspb.Workflow_SourceContents{
					SourceContents: "main:\n  steps:\n    - ret:\n        return: 1",
				},
			},
		})
		if err != nil {
			t.Fatalf("CreateWorkflow(%s): %v", id, err)
		}
	}

	resp, err := client.ListWorkflows(ctx, &workflowspb.ListWorkflowsRequest{
		Parent: parent,
	})
	if err != nil {
		t.Fatalf("ListWorkflows: %v", err)
	}
	if len(resp.GetWorkflows()) != 2 {
		t.Fatalf("expected 2 workflows, got %d", len(resp.GetWorkflows()))
	}
}

func TestDeleteWorkflow(t *testing.T) {
	addr, cleanup := startTestServer(t)
	defer cleanup()

	conn := dial(t, addr)
	defer conn.Close()

	client := workflowspb.NewWorkflowsClient(conn)
	ctx := context.Background()

	name := "projects/my-project/locations/us-central1/workflows/to-delete"

	_, err := client.CreateWorkflow(ctx, &workflowspb.CreateWorkflowRequest{
		Parent:     "projects/my-project/locations/us-central1",
		WorkflowId: "to-delete",
		Workflow: &workflowspb.Workflow{
			SourceCode: &workflowspb.Workflow_SourceContents{
				SourceContents: "main:\n  steps:\n    - ret:\n        return: 1",
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	op, err := client.DeleteWorkflow(ctx, &workflowspb.DeleteWorkflowRequest{
		Name: name,
	})
	if err != nil {
		t.Fatalf("DeleteWorkflow: %v", err)
	}
	if !op.GetDone() {
		t.Fatal("expected delete operation to be done")
	}

	// Verify it's gone
	_, err = client.GetWorkflow(ctx, &workflowspb.GetWorkflowRequest{Name: name})
	if err == nil {
		t.Fatal("expected not-found error after delete")
	}
}

func TestUpdateWorkflow(t *testing.T) {
	addr, cleanup := startTestServer(t)
	defer cleanup()

	conn := dial(t, addr)
	defer conn.Close()

	client := workflowspb.NewWorkflowsClient(conn)
	ctx := context.Background()

	name := "projects/my-project/locations/us-central1/workflows/to-update"

	_, err := client.CreateWorkflow(ctx, &workflowspb.CreateWorkflowRequest{
		Parent:     "projects/my-project/locations/us-central1",
		WorkflowId: "to-update",
		Workflow: &workflowspb.Workflow{
			SourceCode: &workflowspb.Workflow_SourceContents{
				SourceContents: "main:\n  steps:\n    - ret:\n        return: 1",
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	op, err := client.UpdateWorkflow(ctx, &workflowspb.UpdateWorkflowRequest{
		Workflow: &workflowspb.Workflow{
			Name: name,
			SourceCode: &workflowspb.Workflow_SourceContents{
				SourceContents: "main:\n  steps:\n    - ret:\n        return: 2",
			},
		},
	})
	if err != nil {
		t.Fatalf("UpdateWorkflow: %v", err)
	}
	if !op.GetDone() {
		t.Fatal("expected update operation to be done")
	}

	wf, err := client.GetWorkflow(ctx, &workflowspb.GetWorkflowRequest{Name: name})
	if err != nil {
		t.Fatalf("GetWorkflow after update: %v", err)
	}
	if wf.GetSourceContents() != "main:\n  steps:\n    - ret:\n        return: 2" {
		t.Fatalf("source not updated: %s", wf.GetSourceContents())
	}
}

func TestCreateAndGetExecution(t *testing.T) {
	addr, cleanup := startTestServer(t)
	defer cleanup()

	conn := dial(t, addr)
	defer conn.Close()

	wfClient := workflowspb.NewWorkflowsClient(conn)
	exClient := executionspb.NewExecutionsClient(conn)
	ctx := context.Background()

	workflowName := "projects/my-project/locations/us-central1/workflows/exec-test"

	_, err := wfClient.CreateWorkflow(ctx, &workflowspb.CreateWorkflowRequest{
		Parent:     "projects/my-project/locations/us-central1",
		WorkflowId: "exec-test",
		Workflow: &workflowspb.Workflow{
			SourceCode: &workflowspb.Workflow_SourceContents{
				SourceContents: "main:\n  steps:\n    - ret:\n        return: \"done\"",
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	exec, err := exClient.CreateExecution(ctx, &executionspb.CreateExecutionRequest{
		Parent:    workflowName,
		Execution: &executionspb.Execution{},
	})
	if err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}
	if exec.GetName() == "" {
		t.Fatal("expected execution name to be set")
	}

	// Poll for completion
	var got *executionspb.Execution
	for i := 0; i < 50; i++ {
		got, err = exClient.GetExecution(ctx, &executionspb.GetExecutionRequest{
			Name: exec.GetName(),
		})
		if err != nil {
			t.Fatalf("GetExecution: %v", err)
		}
		if got.GetState() != executionspb.Execution_ACTIVE {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if got.GetState() != executionspb.Execution_SUCCEEDED {
		t.Fatalf("expected SUCCEEDED, got %v (error: %v)", got.GetState(), got.GetError())
	}
	if got.GetResult() != `"done"` {
		t.Fatalf("unexpected result: %s", got.GetResult())
	}
}

func TestListExecutions(t *testing.T) {
	addr, cleanup := startTestServer(t)
	defer cleanup()

	conn := dial(t, addr)
	defer conn.Close()

	wfClient := workflowspb.NewWorkflowsClient(conn)
	exClient := executionspb.NewExecutionsClient(conn)
	ctx := context.Background()

	workflowName := "projects/my-project/locations/us-central1/workflows/list-exec"

	_, err := wfClient.CreateWorkflow(ctx, &workflowspb.CreateWorkflowRequest{
		Parent:     "projects/my-project/locations/us-central1",
		WorkflowId: "list-exec",
		Workflow: &workflowspb.Workflow{
			SourceCode: &workflowspb.Workflow_SourceContents{
				SourceContents: "main:\n  steps:\n    - ret:\n        return: 1",
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	// Create 3 executions
	for i := 0; i < 3; i++ {
		_, err := exClient.CreateExecution(ctx, &executionspb.CreateExecutionRequest{
			Parent:    workflowName,
			Execution: &executionspb.Execution{},
		})
		if err != nil {
			t.Fatalf("CreateExecution %d: %v", i, err)
		}
	}

	resp, err := exClient.ListExecutions(ctx, &executionspb.ListExecutionsRequest{
		Parent: workflowName,
	})
	if err != nil {
		t.Fatalf("ListExecutions: %v", err)
	}
	if len(resp.GetExecutions()) != 3 {
		t.Fatalf("expected 3 executions, got %d", len(resp.GetExecutions()))
	}
}

func TestCreateWorkflowErrors(t *testing.T) {
	addr, cleanup := startTestServer(t)
	defer cleanup()

	conn := dial(t, addr)
	defer conn.Close()

	client := workflowspb.NewWorkflowsClient(conn)
	ctx := context.Background()

	// Missing workflow_id
	_, err := client.CreateWorkflow(ctx, &workflowspb.CreateWorkflowRequest{
		Parent:   "projects/my-project/locations/us-central1",
		Workflow: &workflowspb.Workflow{},
	})
	if err == nil {
		t.Fatal("expected error for missing workflow_id")
	}

	// Duplicate
	req := &workflowspb.CreateWorkflowRequest{
		Parent:     "projects/my-project/locations/us-central1",
		WorkflowId: "dup",
		Workflow: &workflowspb.Workflow{
			SourceCode: &workflowspb.Workflow_SourceContents{
				SourceContents: "main:\n  steps:\n    - ret:\n        return: 1",
			},
		},
	}
	_, err = client.CreateWorkflow(ctx, req)
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err = client.CreateWorkflow(ctx, req)
	if err == nil {
		t.Fatal("expected AlreadyExists error")
	}
}

func TestExecutionWithArguments(t *testing.T) {
	addr, cleanup := startTestServer(t)
	defer cleanup()

	conn := dial(t, addr)
	defer conn.Close()

	wfClient := workflowspb.NewWorkflowsClient(conn)
	exClient := executionspb.NewExecutionsClient(conn)
	ctx := context.Background()

	workflowName := "projects/my-project/locations/us-central1/workflows/args-test"

	_, err := wfClient.CreateWorkflow(ctx, &workflowspb.CreateWorkflowRequest{
		Parent:     "projects/my-project/locations/us-central1",
		WorkflowId: "args-test",
		Workflow: &workflowspb.Workflow{
			SourceCode: &workflowspb.Workflow_SourceContents{
				SourceContents: "main:\n  params: [args]\n  steps:\n    - ret:\n        return: ${args.greeting}",
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	exec, err := exClient.CreateExecution(ctx, &executionspb.CreateExecutionRequest{
		Parent: workflowName,
		Execution: &executionspb.Execution{
			Argument: `{"greeting":"hello world"}`,
		},
	})
	if err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}

	// Poll for completion
	var got *executionspb.Execution
	for i := 0; i < 50; i++ {
		got, err = exClient.GetExecution(ctx, &executionspb.GetExecutionRequest{
			Name: exec.GetName(),
		})
		if err != nil {
			t.Fatalf("GetExecution: %v", err)
		}
		if got.GetState() != executionspb.Execution_ACTIVE {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if got.GetState() != executionspb.Execution_SUCCEEDED {
		t.Fatalf("expected SUCCEEDED, got %v (error: %v)", got.GetState(), got.GetError())
	}
	if got.GetResult() != `"hello world"` {
		t.Fatalf("unexpected result: got %s, want %q", got.GetResult(), "hello world")
	}
}
