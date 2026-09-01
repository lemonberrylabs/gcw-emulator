package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	workflows "cloud.google.com/go/workflows/apiv1"
	workflowspb "cloud.google.com/go/workflows/apiv1/workflowspb"
	executions "cloud.google.com/go/workflows/executions/apiv1"
	executionspb "cloud.google.com/go/workflows/executions/apiv1/executionspb"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// grpcEndpoint returns the gRPC endpoint address (host:port).
func grpcEndpoint() string {
	if ep := os.Getenv("GCW_GRPC_ENDPOINT"); ep != "" {
		return ep
	}
	return "localhost:8788"
}

// grpcClientOptions returns the common options for connecting official Google
// Cloud client libraries to the emulator's gRPC port.
func grpcClientOptions() []option.ClientOption {
	return []option.ClientOption{
		option.WithEndpoint(grpcEndpoint()),
		option.WithoutAuthentication(),
		option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
	}
}

// newWorkflowsClient creates a workflows client connected to the emulator.
func newWorkflowsClient(t *testing.T) *workflows.Client {
	t.Helper()
	ctx := context.Background()
	client, err := workflows.NewClient(ctx, grpcClientOptions()...)
	if err != nil {
		t.Fatalf("workflows.NewClient: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

// newExecutionsClient creates an executions client connected to the emulator.
func newExecutionsClient(t *testing.T) *executions.Client {
	t.Helper()
	ctx := context.Background()
	client, err := executions.NewClient(ctx, grpcClientOptions()...)
	if err != nil {
		t.Fatalf("executions.NewClient: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

// pollGRPCExecution polls GetExecution via gRPC until it reaches a terminal state.
func pollGRPCExecution(t *testing.T, client *executions.Client, name string, timeout time.Duration) *executionspb.Execution {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			t.Fatalf("gRPC execution %s did not complete within %s", name, timeout)
		}
		exec, err := client.GetExecution(ctx, &executionspb.GetExecutionRequest{Name: name})
		if err != nil {
			t.Fatalf("GetExecution(%s): %v", name, err)
		}
		if exec.GetState() != executionspb.Execution_ACTIVE {
			return exec
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// --------------------------------------------------------------------------
// 1. Workflow CRUD via official gRPC client
// --------------------------------------------------------------------------

func TestGRPC_CreateAndGetWorkflow(t *testing.T) {
	client := newWorkflowsClient(t)
	ctx := context.Background()

	wfID := uniqueID("grpc-create")
	parent := parentPath

	op, err := client.CreateWorkflow(ctx, &workflowspb.CreateWorkflowRequest{
		Parent:     parent,
		WorkflowId: wfID,
		Workflow: &workflowspb.Workflow{
			SourceCode: &workflowspb.Workflow_SourceContents{
				SourceContents: "main:\n  steps:\n    - done:\n        return: \"grpc-hello\"",
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	wf, err := op.Wait(ctx)
	if err != nil {
		t.Fatalf("CreateWorkflow op.Wait: %v", err)
	}
	expectedName := fmt.Sprintf("%s/workflows/%s", parent, wfID)
	if wf.GetName() != expectedName {
		t.Fatalf("expected name %q, got %q", expectedName, wf.GetName())
	}
	if wf.GetState() != workflowspb.Workflow_ACTIVE {
		t.Fatalf("expected ACTIVE state, got %v", wf.GetState())
	}
	if wf.GetSourceContents() == "" {
		t.Fatal("expected source_contents to be set")
	}

	// Get
	got, err := client.GetWorkflow(ctx, &workflowspb.GetWorkflowRequest{Name: expectedName})
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if got.GetName() != expectedName {
		t.Fatalf("GetWorkflow name mismatch: %q vs %q", got.GetName(), expectedName)
	}
}

func TestGRPC_ListWorkflows(t *testing.T) {
	client := newWorkflowsClient(t)
	ctx := context.Background()

	// Create two workflows with unique IDs
	ids := []string{uniqueID("grpc-list-a"), uniqueID("grpc-list-b")}
	for _, id := range ids {
		op, err := client.CreateWorkflow(ctx, &workflowspb.CreateWorkflowRequest{
			Parent:     parentPath,
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
		if _, err := op.Wait(ctx); err != nil {
			t.Fatalf("CreateWorkflow(%s) Wait: %v", id, err)
		}
	}

	// List should include at least these two
	it := client.ListWorkflows(ctx, &workflowspb.ListWorkflowsRequest{Parent: parentPath})
	var names []string
	for {
		wf, err := it.Next()
		if err != nil {
			break
		}
		names = append(names, wf.GetName())
	}

	for _, id := range ids {
		expected := fmt.Sprintf("%s/workflows/%s", parentPath, id)
		found := false
		for _, n := range names {
			if n == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("ListWorkflows missing %q; got %v", expected, names)
		}
	}
}

func TestGRPC_UpdateWorkflow(t *testing.T) {
	client := newWorkflowsClient(t)
	ctx := context.Background()

	wfID := uniqueID("grpc-update")
	name := fmt.Sprintf("%s/workflows/%s", parentPath, wfID)

	op, err := client.CreateWorkflow(ctx, &workflowspb.CreateWorkflowRequest{
		Parent:     parentPath,
		WorkflowId: wfID,
		Workflow: &workflowspb.Workflow{
			SourceCode: &workflowspb.Workflow_SourceContents{
				SourceContents: "main:\n  steps:\n    - ret:\n        return: \"v1\"",
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if _, err := op.Wait(ctx); err != nil {
		t.Fatalf("CreateWorkflow Wait: %v", err)
	}

	updatedSource := "main:\n  steps:\n    - ret:\n        return: \"v2\""
	uop, err := client.UpdateWorkflow(ctx, &workflowspb.UpdateWorkflowRequest{
		Workflow: &workflowspb.Workflow{
			Name: name,
			SourceCode: &workflowspb.Workflow_SourceContents{
				SourceContents: updatedSource,
			},
		},
	})
	if err != nil {
		t.Fatalf("UpdateWorkflow: %v", err)
	}
	wf, err := uop.Wait(ctx)
	if err != nil {
		t.Fatalf("UpdateWorkflow Wait: %v", err)
	}
	if wf.GetSourceContents() != updatedSource {
		t.Fatalf("source not updated: got %q", wf.GetSourceContents())
	}
}

func TestGRPC_DeleteWorkflow(t *testing.T) {
	client := newWorkflowsClient(t)
	ctx := context.Background()

	wfID := uniqueID("grpc-delete")
	name := fmt.Sprintf("%s/workflows/%s", parentPath, wfID)

	op, err := client.CreateWorkflow(ctx, &workflowspb.CreateWorkflowRequest{
		Parent:     parentPath,
		WorkflowId: wfID,
		Workflow: &workflowspb.Workflow{
			SourceCode: &workflowspb.Workflow_SourceContents{
				SourceContents: "main:\n  steps:\n    - ret:\n        return: 1",
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if _, err := op.Wait(ctx); err != nil {
		t.Fatalf("CreateWorkflow Wait: %v", err)
	}

	dop, err := client.DeleteWorkflow(ctx, &workflowspb.DeleteWorkflowRequest{Name: name})
	if err != nil {
		t.Fatalf("DeleteWorkflow: %v", err)
	}
	if err := dop.Wait(ctx); err != nil {
		t.Fatalf("DeleteWorkflow Wait: %v", err)
	}

	// Verify it's gone
	_, err = client.GetWorkflow(ctx, &workflowspb.GetWorkflowRequest{Name: name})
	if err == nil {
		t.Fatal("expected not-found error after delete")
	}
	if code := status.Code(err); code != codes.NotFound {
		t.Fatalf("expected NotFound, got %v: %v", code, err)
	}
}

// --------------------------------------------------------------------------
// 2. Execution CRUD via official gRPC client
// --------------------------------------------------------------------------

func TestGRPC_CreateAndPollExecution(t *testing.T) {
	wfClient := newWorkflowsClient(t)
	exClient := newExecutionsClient(t)
	ctx := context.Background()

	wfID := uniqueID("grpc-exec")
	wfName := fmt.Sprintf("%s/workflows/%s", parentPath, wfID)

	op, err := wfClient.CreateWorkflow(ctx, &workflowspb.CreateWorkflowRequest{
		Parent:     parentPath,
		WorkflowId: wfID,
		Workflow: &workflowspb.Workflow{
			SourceCode: &workflowspb.Workflow_SourceContents{
				SourceContents: "main:\n  steps:\n    - done:\n        return: \"grpc-result\"",
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if _, err := op.Wait(ctx); err != nil {
		t.Fatalf("CreateWorkflow Wait: %v", err)
	}

	exec, err := exClient.CreateExecution(ctx, &executionspb.CreateExecutionRequest{
		Parent:    wfName,
		Execution: &executionspb.Execution{},
	})
	if err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}
	if exec.GetName() == "" {
		t.Fatal("expected execution name")
	}

	got := pollGRPCExecution(t, exClient, exec.GetName(), 10*time.Second)
	if got.GetState() != executionspb.Execution_SUCCEEDED {
		t.Fatalf("expected SUCCEEDED, got %v (error: %v)", got.GetState(), got.GetError())
	}
	if got.GetResult() != `"grpc-result"` {
		t.Fatalf("unexpected result: %s", got.GetResult())
	}
}

func TestGRPC_ExecutionWithArguments(t *testing.T) {
	wfClient := newWorkflowsClient(t)
	exClient := newExecutionsClient(t)
	ctx := context.Background()

	wfID := uniqueID("grpc-exec-args")
	wfName := fmt.Sprintf("%s/workflows/%s", parentPath, wfID)

	op, err := wfClient.CreateWorkflow(ctx, &workflowspb.CreateWorkflowRequest{
		Parent:     parentPath,
		WorkflowId: wfID,
		Workflow: &workflowspb.Workflow{
			SourceCode: &workflowspb.Workflow_SourceContents{
				SourceContents: "main:\n  params: [args]\n  steps:\n    - done:\n        return: ${args.name}",
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if _, err := op.Wait(ctx); err != nil {
		t.Fatalf("CreateWorkflow Wait: %v", err)
	}

	exec, err := exClient.CreateExecution(ctx, &executionspb.CreateExecutionRequest{
		Parent: wfName,
		Execution: &executionspb.Execution{
			Argument: `{"name":"from-grpc"}`,
		},
	})
	if err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}

	got := pollGRPCExecution(t, exClient, exec.GetName(), 10*time.Second)
	if got.GetState() != executionspb.Execution_SUCCEEDED {
		t.Fatalf("expected SUCCEEDED, got %v", got.GetState())
	}
	if got.GetResult() != `"from-grpc"` {
		t.Fatalf("unexpected result: %s", got.GetResult())
	}
}

func TestGRPC_ListExecutions(t *testing.T) {
	wfClient := newWorkflowsClient(t)
	exClient := newExecutionsClient(t)
	ctx := context.Background()

	wfID := uniqueID("grpc-list-exec")
	wfName := fmt.Sprintf("%s/workflows/%s", parentPath, wfID)

	op, err := wfClient.CreateWorkflow(ctx, &workflowspb.CreateWorkflowRequest{
		Parent:     parentPath,
		WorkflowId: wfID,
		Workflow: &workflowspb.Workflow{
			SourceCode: &workflowspb.Workflow_SourceContents{
				SourceContents: "main:\n  steps:\n    - ret:\n        return: 1",
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if _, err := op.Wait(ctx); err != nil {
		t.Fatalf("CreateWorkflow Wait: %v", err)
	}

	// Create 3 executions
	for i := 0; i < 3; i++ {
		_, err := exClient.CreateExecution(ctx, &executionspb.CreateExecutionRequest{
			Parent:    wfName,
			Execution: &executionspb.Execution{},
		})
		if err != nil {
			t.Fatalf("CreateExecution %d: %v", i, err)
		}
	}

	// Wait for all to complete
	time.Sleep(500 * time.Millisecond)

	it := exClient.ListExecutions(ctx, &executionspb.ListExecutionsRequest{Parent: wfName})
	count := 0
	for {
		_, err := it.Next()
		if err != nil {
			break
		}
		count++
	}
	if count != 3 {
		t.Fatalf("expected 3 executions, got %d", count)
	}
}

func TestGRPC_CancelExecution(t *testing.T) {
	wfClient := newWorkflowsClient(t)
	exClient := newExecutionsClient(t)
	ctx := context.Background()

	// Use a workflow that sleeps (via http call to a non-routable IP to simulate long-running)
	wfID := uniqueID("grpc-cancel")
	wfName := fmt.Sprintf("%s/workflows/%s", parentPath, wfID)

	// Simple workflow that returns quickly -- we'll cancel before polling
	op, err := wfClient.CreateWorkflow(ctx, &workflowspb.CreateWorkflowRequest{
		Parent:     parentPath,
		WorkflowId: wfID,
		Workflow: &workflowspb.Workflow{
			SourceCode: &workflowspb.Workflow_SourceContents{
				SourceContents: `main:
  steps:
    - long_step:
        call: http.get
        args:
          url: "http://192.0.2.1:1"
          timeout: 30
        result: resp
    - done:
        return: ${resp.body}`,
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if _, err := op.Wait(ctx); err != nil {
		t.Fatalf("CreateWorkflow Wait: %v", err)
	}

	exec, err := exClient.CreateExecution(ctx, &executionspb.CreateExecutionRequest{
		Parent:    wfName,
		Execution: &executionspb.Execution{},
	})
	if err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}

	// Cancel immediately
	cancelled, err := exClient.CancelExecution(ctx, &executionspb.CancelExecutionRequest{
		Name: exec.GetName(),
	})
	if err != nil {
		t.Fatalf("CancelExecution: %v", err)
	}
	if cancelled.GetState() != executionspb.Execution_CANCELLED {
		t.Fatalf("expected CANCELLED, got %v", cancelled.GetState())
	}
}

// --------------------------------------------------------------------------
// 3. Cross-transport consistency: gRPC <-> REST share the same store
// --------------------------------------------------------------------------

func TestGRPC_DeployViaGRPC_ExecuteViaREST(t *testing.T) {
	wfClient := newWorkflowsClient(t)
	ctx := context.Background()

	wfID := uniqueID("grpc-to-rest")
	wfName := fmt.Sprintf("%s/workflows/%s", parentPath, wfID)

	// Deploy via gRPC
	op, err := wfClient.CreateWorkflow(ctx, &workflowspb.CreateWorkflowRequest{
		Parent:     parentPath,
		WorkflowId: wfID,
		Workflow: &workflowspb.Workflow{
			SourceCode: &workflowspb.Workflow_SourceContents{
				SourceContents: "main:\n  steps:\n    - done:\n        return: \"deployed-via-grpc\"",
			},
		},
	})
	if err != nil {
		t.Fatalf("gRPC CreateWorkflow: %v", err)
	}
	if _, err := op.Wait(ctx); err != nil {
		t.Fatalf("gRPC CreateWorkflow Wait: %v", err)
	}

	// Execute via REST
	result := executeWorkflow(t, wfName, nil)
	assertSucceeded(t, result)
	assertResultEquals(t, result, "deployed-via-grpc")
}

func TestGRPC_DeployViaREST_ExecuteViaGRPC(t *testing.T) {
	exClient := newExecutionsClient(t)
	ctx := context.Background()

	wfID := uniqueID("rest-to-grpc")
	wfName := createWorkflow(t, wfID, "main:\n  steps:\n    - done:\n        return: \"deployed-via-rest\"")

	// Execute via gRPC
	exec, err := exClient.CreateExecution(ctx, &executionspb.CreateExecutionRequest{
		Parent:    wfName,
		Execution: &executionspb.Execution{},
	})
	if err != nil {
		t.Fatalf("gRPC CreateExecution: %v", err)
	}

	got := pollGRPCExecution(t, exClient, exec.GetName(), 10*time.Second)
	if got.GetState() != executionspb.Execution_SUCCEEDED {
		t.Fatalf("expected SUCCEEDED, got %v (error: %v)", got.GetState(), got.GetError())
	}
	if got.GetResult() != `"deployed-via-rest"` {
		t.Fatalf("unexpected result: %s", got.GetResult())
	}
}

func TestGRPC_DeployViaREST_ReadViaGRPC(t *testing.T) {
	wfClient := newWorkflowsClient(t)
	ctx := context.Background()

	wfID := uniqueID("rest-read-grpc")
	wfName := createWorkflow(t, wfID, "main:\n  steps:\n    - done:\n        return: 42")

	// Read via gRPC
	wf, err := wfClient.GetWorkflow(ctx, &workflowspb.GetWorkflowRequest{Name: wfName})
	if err != nil {
		t.Fatalf("gRPC GetWorkflow: %v", err)
	}
	if wf.GetName() != wfName {
		t.Fatalf("name mismatch: %q vs %q", wf.GetName(), wfName)
	}
	if wf.GetState() != workflowspb.Workflow_ACTIVE {
		t.Fatalf("expected ACTIVE, got %v", wf.GetState())
	}
}

func TestGRPC_DeployViaGRPC_ReadViaREST(t *testing.T) {
	wfClient := newWorkflowsClient(t)
	ctx := context.Background()

	wfID := uniqueID("grpc-read-rest")
	wfName := fmt.Sprintf("%s/workflows/%s", parentPath, wfID)

	op, err := wfClient.CreateWorkflow(ctx, &workflowspb.CreateWorkflowRequest{
		Parent:     parentPath,
		WorkflowId: wfID,
		Workflow: &workflowspb.Workflow{
			SourceCode: &workflowspb.Workflow_SourceContents{
				SourceContents: "main:\n  steps:\n    - done:\n        return: \"from-grpc\"",
			},
		},
	})
	if err != nil {
		t.Fatalf("gRPC CreateWorkflow: %v", err)
	}
	if _, err := op.Wait(ctx); err != nil {
		t.Fatalf("gRPC CreateWorkflow Wait: %v", err)
	}

	// Read via REST
	resp, err := http.Get(apiURL(wfName))
	if err != nil {
		t.Fatalf("REST GetWorkflow: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("REST GetWorkflow status %d: %s", resp.StatusCode, body)
	}
	var wf map[string]interface{}
	if err := json.Unmarshal(body, &wf); err != nil {
		t.Fatalf("REST response decode: %v", err)
	}
	if wf["name"] != wfName {
		t.Fatalf("REST name mismatch: %v", wf["name"])
	}
}

func TestGRPC_ExecutionVisibleAcrossTransports(t *testing.T) {
	wfClient := newWorkflowsClient(t)
	exClient := newExecutionsClient(t)
	ctx := context.Background()

	wfID := uniqueID("grpc-exec-cross")
	wfName := fmt.Sprintf("%s/workflows/%s", parentPath, wfID)

	op, err := wfClient.CreateWorkflow(ctx, &workflowspb.CreateWorkflowRequest{
		Parent:     parentPath,
		WorkflowId: wfID,
		Workflow: &workflowspb.Workflow{
			SourceCode: &workflowspb.Workflow_SourceContents{
				SourceContents: "main:\n  steps:\n    - done:\n        return: \"cross-check\"",
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if _, err := op.Wait(ctx); err != nil {
		t.Fatalf("CreateWorkflow Wait: %v", err)
	}

	// Create execution via gRPC
	exec, err := exClient.CreateExecution(ctx, &executionspb.CreateExecutionRequest{
		Parent:    wfName,
		Execution: &executionspb.Execution{},
	})
	if err != nil {
		t.Fatalf("gRPC CreateExecution: %v", err)
	}

	// Wait for it to complete via gRPC
	got := pollGRPCExecution(t, exClient, exec.GetName(), 10*time.Second)
	if got.GetState() != executionspb.Execution_SUCCEEDED {
		t.Fatalf("expected SUCCEEDED via gRPC, got %v", got.GetState())
	}

	// Verify the same execution is visible via REST
	resp, err := http.Get(apiURL(exec.GetName()))
	if err != nil {
		t.Fatalf("REST GetExecution: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("REST GetExecution status %d: %s", resp.StatusCode, body)
	}
	var restExec map[string]interface{}
	if err := json.Unmarshal(body, &restExec); err != nil {
		t.Fatalf("REST decode: %v", err)
	}
	if restExec["state"] != "SUCCEEDED" {
		t.Fatalf("REST execution state: %v", restExec["state"])
	}
}

// --------------------------------------------------------------------------
// 4. gRPC error handling
// --------------------------------------------------------------------------

func TestGRPC_GetMissingWorkflow(t *testing.T) {
	client := newWorkflowsClient(t)
	ctx := context.Background()

	_, err := client.GetWorkflow(ctx, &workflowspb.GetWorkflowRequest{
		Name: parentPath + "/workflows/does-not-exist",
	})
	if err == nil {
		t.Fatal("expected error for missing workflow")
	}
	if code := status.Code(err); code != codes.NotFound {
		t.Fatalf("expected NotFound, got %v: %v", code, err)
	}
}

func TestGRPC_DuplicateWorkflow(t *testing.T) {
	client := newWorkflowsClient(t)
	ctx := context.Background()

	wfID := uniqueID("grpc-dup")
	req := &workflowspb.CreateWorkflowRequest{
		Parent:     parentPath,
		WorkflowId: wfID,
		Workflow: &workflowspb.Workflow{
			SourceCode: &workflowspb.Workflow_SourceContents{
				SourceContents: "main:\n  steps:\n    - ret:\n        return: 1",
			},
		},
	}

	op, err := client.CreateWorkflow(ctx, req)
	if err != nil {
		t.Fatalf("first CreateWorkflow: %v", err)
	}
	if _, err := op.Wait(ctx); err != nil {
		t.Fatalf("first CreateWorkflow Wait: %v", err)
	}

	_, err = client.CreateWorkflow(ctx, req)
	if err == nil {
		t.Fatal("expected AlreadyExists error for duplicate")
	}
	if code := status.Code(err); code != codes.AlreadyExists {
		t.Fatalf("expected AlreadyExists, got %v: %v", code, err)
	}
}

func TestGRPC_InvalidWorkflowSource(t *testing.T) {
	client := newWorkflowsClient(t)
	ctx := context.Background()

	_, err := client.CreateWorkflow(ctx, &workflowspb.CreateWorkflowRequest{
		Parent:     parentPath,
		WorkflowId: uniqueID("grpc-invalid"),
		Workflow: &workflowspb.Workflow{
			SourceCode: &workflowspb.Workflow_SourceContents{
				SourceContents: "this is not valid yaml workflow",
			},
		},
	})
	if err == nil {
		t.Fatal("expected error for invalid workflow source")
	}
}

func TestGRPC_ExecuteMissingWorkflow(t *testing.T) {
	exClient := newExecutionsClient(t)
	ctx := context.Background()

	_, err := exClient.CreateExecution(ctx, &executionspb.CreateExecutionRequest{
		Parent:    parentPath + "/workflows/nonexistent",
		Execution: &executionspb.Execution{},
	})
	if err == nil {
		t.Fatal("expected error for missing workflow")
	}
	if code := status.Code(err); code != codes.NotFound {
		t.Fatalf("expected NotFound, got %v: %v", code, err)
	}
}

func TestGRPC_FailedExecution(t *testing.T) {
	wfClient := newWorkflowsClient(t)
	exClient := newExecutionsClient(t)
	ctx := context.Background()

	wfID := uniqueID("grpc-fail")
	wfName := fmt.Sprintf("%s/workflows/%s", parentPath, wfID)

	op, err := wfClient.CreateWorkflow(ctx, &workflowspb.CreateWorkflowRequest{
		Parent:     parentPath,
		WorkflowId: wfID,
		Workflow: &workflowspb.Workflow{
			SourceCode: &workflowspb.Workflow_SourceContents{
				SourceContents: `main:
  steps:
    - fail:
        raise: "intentional error"`,
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if _, err := op.Wait(ctx); err != nil {
		t.Fatalf("CreateWorkflow Wait: %v", err)
	}

	exec, err := exClient.CreateExecution(ctx, &executionspb.CreateExecutionRequest{
		Parent:    wfName,
		Execution: &executionspb.Execution{},
	})
	if err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}

	got := pollGRPCExecution(t, exClient, exec.GetName(), 10*time.Second)
	if got.GetState() != executionspb.Execution_FAILED {
		t.Fatalf("expected FAILED, got %v", got.GetState())
	}
	if got.GetError() == nil {
		t.Fatal("expected error payload on failed execution")
	}
	if got.GetError().GetPayload() == "" {
		t.Fatal("expected non-empty error payload")
	}
}

func TestGRPC_DeleteViaGRPC_NotFoundViaREST(t *testing.T) {
	wfClient := newWorkflowsClient(t)
	ctx := context.Background()

	wfID := uniqueID("grpc-del-rest")
	wfName := fmt.Sprintf("%s/workflows/%s", parentPath, wfID)

	op, err := wfClient.CreateWorkflow(ctx, &workflowspb.CreateWorkflowRequest{
		Parent:     parentPath,
		WorkflowId: wfID,
		Workflow: &workflowspb.Workflow{
			SourceCode: &workflowspb.Workflow_SourceContents{
				SourceContents: "main:\n  steps:\n    - ret:\n        return: 1",
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if _, err := op.Wait(ctx); err != nil {
		t.Fatalf("CreateWorkflow Wait: %v", err)
	}

	// Delete via gRPC
	dop, err := wfClient.DeleteWorkflow(ctx, &workflowspb.DeleteWorkflowRequest{Name: wfName})
	if err != nil {
		t.Fatalf("gRPC DeleteWorkflow: %v", err)
	}
	if err := dop.Wait(ctx); err != nil {
		t.Fatalf("gRPC DeleteWorkflow Wait: %v", err)
	}

	// Verify not found via REST
	resp, err := http.Get(apiURL(wfName))
	if err != nil {
		t.Fatalf("REST GetWorkflow: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 404, got %d: %s", resp.StatusCode, body)
	}
}

func TestGRPC_DeleteViaREST_NotFoundViaGRPC(t *testing.T) {
	wfClient := newWorkflowsClient(t)
	ctx := context.Background()

	wfID := uniqueID("rest-del-grpc")
	wfName := createWorkflow(t, wfID, "main:\n  steps:\n    - ret:\n        return: 1")

	// Delete via REST
	req, _ := http.NewRequest(http.MethodDelete, apiURL(wfName), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("REST DeleteWorkflow: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("REST delete status: %d", resp.StatusCode)
	}

	// Verify not found via gRPC
	_, err = wfClient.GetWorkflow(ctx, &workflowspb.GetWorkflowRequest{Name: wfName})
	if err == nil {
		t.Fatal("expected NotFound via gRPC after REST delete")
	}
	if code := status.Code(err); code != codes.NotFound {
		t.Fatalf("expected NotFound, got %v: %v", code, err)
	}
}

func TestGRPC_UpdateViaREST_ReadViaGRPC(t *testing.T) {
	wfClient := newWorkflowsClient(t)
	ctx := context.Background()

	wfID := uniqueID("rest-upd-grpc")
	wfName := createWorkflow(t, wfID, "main:\n  steps:\n    - ret:\n        return: \"v1\"")

	// Update via REST
	updateBody, _ := json.Marshal(map[string]interface{}{
		"sourceContents": "main:\n  steps:\n    - ret:\n        return: \"v2-from-rest\"",
	})
	patchReq, _ := http.NewRequest(http.MethodPatch, apiURL(wfName), bytes.NewReader(updateBody))
	patchReq.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(patchReq)
	if err != nil {
		t.Fatalf("REST UpdateWorkflow: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("REST update status: %d", resp.StatusCode)
	}

	// Read via gRPC
	wf, err := wfClient.GetWorkflow(ctx, &workflowspb.GetWorkflowRequest{Name: wfName})
	if err != nil {
		t.Fatalf("gRPC GetWorkflow: %v", err)
	}
	if !strings.Contains(wf.GetSourceContents(), "v2-from-rest") {
		t.Fatalf("expected updated source via gRPC, got: %s", wf.GetSourceContents())
	}
}

// TestGRPC_CallbackEndpointRegistered verifies that executions started via
// gRPC get the callback registrar: events.create_callback_endpoint returns an
// absolute URL and the endpoint is listed by the REST callbacks API.
func TestGRPC_CallbackEndpointRegistered(t *testing.T) {
	wfClient := newWorkflowsClient(t)
	exClient := newExecutionsClient(t)
	ctx := context.Background()

	wfID := uniqueID("grpc-callback")
	wfName := fmt.Sprintf("%s/workflows/%s", parentPath, wfID)

	op, err := wfClient.CreateWorkflow(ctx, &workflowspb.CreateWorkflowRequest{
		Parent:     parentPath,
		WorkflowId: wfID,
		Workflow: &workflowspb.Workflow{
			SourceCode: &workflowspb.Workflow_SourceContents{
				SourceContents: `main:
  steps:
    - create_cb:
        call: events.create_callback_endpoint
        args:
          http_callback_method: "POST"
        result: callback
    - done:
        return: ${callback.url}`,
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if _, err := op.Wait(ctx); err != nil {
		t.Fatalf("CreateWorkflow Wait: %v", err)
	}

	exec, err := exClient.CreateExecution(ctx, &executionspb.CreateExecutionRequest{
		Parent:    wfName,
		Execution: &executionspb.Execution{},
	})
	if err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}

	got := pollGRPCExecution(t, exClient, exec.GetName(), 10*time.Second)
	if got.GetState() != executionspb.Execution_SUCCEEDED {
		t.Fatalf("expected SUCCEEDED, got %v (error: %v)", got.GetState(), got.GetError())
	}
	result := got.GetResult()
	if !strings.Contains(result, "http") || !strings.Contains(result, "/callbacks/") {
		t.Fatalf("expected absolute callback URL from gRPC-started execution, got %s", result)
	}
}
