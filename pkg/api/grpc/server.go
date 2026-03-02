// Package grpcapi implements gRPC services for the GCW emulator, allowing
// use of official Google Cloud Go client libraries against the emulator.
package grpcapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	longrunningpb "cloud.google.com/go/longrunning/autogen/longrunningpb"
	executionspb "cloud.google.com/go/workflows/executions/apiv1/executionspb"
	workflowspb "cloud.google.com/go/workflows/apiv1/workflowspb"

	"github.com/lemonberrylabs/gcw-emulator/pkg/ast"
	"github.com/lemonberrylabs/gcw-emulator/pkg/parser"
	"github.com/lemonberrylabs/gcw-emulator/pkg/runtime"
	"github.com/lemonberrylabs/gcw-emulator/pkg/stdlib"
	"github.com/lemonberrylabs/gcw-emulator/pkg/store"
	"github.com/lemonberrylabs/gcw-emulator/pkg/types"
)

// Server implements the Workflows and Executions gRPC services.
type Server struct {
	workflowspb.UnimplementedWorkflowsServer
	executionspb.UnimplementedExecutionsServer
	longrunningpb.UnimplementedOperationsServer

	store   *store.Store
	parsed  map[string]*ast.Workflow
	engines map[string]*runtime.Engine
	grpc    *grpc.Server
}

// New creates a new gRPC server wrapping the given store.
func New(s *store.Store) *Server {
	srv := &Server{
		store:   s,
		parsed:  make(map[string]*ast.Workflow),
		engines: make(map[string]*runtime.Engine),
	}

	gs := grpc.NewServer()
	workflowspb.RegisterWorkflowsServer(gs, srv)
	executionspb.RegisterExecutionsServer(gs, srv)
	longrunningpb.RegisterOperationsServer(gs, srv)
	srv.grpc = gs

	return srv
}

// Serve starts listening on the given address and serves gRPC requests.
func (s *Server) Serve(addr string) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("grpc listen: %w", err)
	}
	return s.grpc.Serve(lis)
}

// GracefulStop gracefully stops the gRPC server.
func (s *Server) GracefulStop() {
	s.grpc.GracefulStop()
}

// --- Workflows Service ---

func (s *Server) CreateWorkflow(ctx context.Context, req *workflowspb.CreateWorkflowRequest) (*longrunningpb.Operation, error) {
	if req.GetWorkflowId() == "" {
		return nil, status.Error(codes.InvalidArgument, "workflow_id is required")
	}
	wfProto := req.GetWorkflow()
	if wfProto == nil {
		return nil, status.Error(codes.InvalidArgument, "workflow is required")
	}
	src := wfProto.GetSourceContents()
	if src == "" {
		return nil, status.Error(codes.InvalidArgument, "source_contents is required")
	}

	// Validate by parsing
	wfAST, err := parser.Parse([]byte(src))
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid workflow definition: %v", err)
	}

	wf, err := s.store.CreateWorkflow(req.GetParent(), req.GetWorkflowId(), src, wfProto.GetDescription())
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			return nil, status.Error(codes.AlreadyExists, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}

	s.parsed[wf.Name] = wfAST

	return doneOperation("create-"+req.GetWorkflowId(), storeWorkflowToProto(wf))
}

func (s *Server) GetWorkflow(ctx context.Context, req *workflowspb.GetWorkflowRequest) (*workflowspb.Workflow, error) {
	wf, err := s.store.GetWorkflow(req.GetName())
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	return storeWorkflowToProto(wf), nil
}

func (s *Server) ListWorkflows(ctx context.Context, req *workflowspb.ListWorkflowsRequest) (*workflowspb.ListWorkflowsResponse, error) {
	workflows := s.store.ListWorkflows(req.GetParent())

	pbWorkflows := make([]*workflowspb.Workflow, len(workflows))
	for i, wf := range workflows {
		pbWorkflows[i] = storeWorkflowToProto(wf)
	}

	return &workflowspb.ListWorkflowsResponse{
		Workflows: pbWorkflows,
	}, nil
}

func (s *Server) UpdateWorkflow(ctx context.Context, req *workflowspb.UpdateWorkflowRequest) (*longrunningpb.Operation, error) {
	wfProto := req.GetWorkflow()
	if wfProto == nil {
		return nil, status.Error(codes.InvalidArgument, "workflow is required")
	}

	name := wfProto.GetName()
	src := wfProto.GetSourceContents()

	if src != "" {
		wfAST, err := parser.Parse([]byte(src))
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid workflow definition: %v", err)
		}
		s.parsed[name] = wfAST
	}

	wf, err := s.store.UpdateWorkflow(name, src, wfProto.GetDescription())
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}

	// Extract workflow ID from name for the operation name
	parts := strings.Split(name, "/")
	wfID := parts[len(parts)-1]
	return doneOperation("update-"+wfID, storeWorkflowToProto(wf))
}

func (s *Server) DeleteWorkflow(ctx context.Context, req *workflowspb.DeleteWorkflowRequest) (*longrunningpb.Operation, error) {
	name := req.GetName()
	err := s.store.DeleteWorkflow(name)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}

	delete(s.parsed, name)

	parts := strings.Split(name, "/")
	wfID := parts[len(parts)-1]

	return doneOperation("delete-"+wfID, &emptypb.Empty{})
}

// --- Executions Service ---

func (s *Server) CreateExecution(ctx context.Context, req *executionspb.CreateExecutionRequest) (*executionspb.Execution, error) {
	workflowName := req.GetParent()

	execProto := req.GetExecution()

	var args types.Value = types.Null
	if execProto != nil && execProto.GetArgument() != "" {
		var raw interface{}
		if err := json.Unmarshal([]byte(execProto.GetArgument()), &raw); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid argument JSON: %v", err)
		}
		args = types.ValueFromJSON(raw)
	}

	// Get parsed workflow
	wfAST, ok := s.parsed[workflowName]
	if !ok {
		wf, err := s.store.GetWorkflow(workflowName)
		if err != nil {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		parsed, err := parser.Parse([]byte(wf.SourceCode))
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to parse workflow: %v", err)
		}
		wfAST = parsed
		s.parsed[workflowName] = wfAST
	}

	exec, err := s.store.CreateExecution(workflowName, args)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Execute asynchronously
	go s.runExecution(exec.Name, wfAST, args)

	return storeExecutionToProto(exec), nil
}

func (s *Server) GetExecution(ctx context.Context, req *executionspb.GetExecutionRequest) (*executionspb.Execution, error) {
	exec, err := s.store.GetExecution(req.GetName())
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	return storeExecutionToProto(exec), nil
}

func (s *Server) ListExecutions(ctx context.Context, req *executionspb.ListExecutionsRequest) (*executionspb.ListExecutionsResponse, error) {
	executions := s.store.ListExecutions(req.GetParent())

	pbExecs := make([]*executionspb.Execution, len(executions))
	for i, exec := range executions {
		pbExecs[i] = storeExecutionToProto(exec)
	}

	return &executionspb.ListExecutionsResponse{
		Executions: pbExecs,
	}, nil
}

func (s *Server) CancelExecution(ctx context.Context, req *executionspb.CancelExecutionRequest) (*executionspb.Execution, error) {
	name := req.GetName()

	if engine, ok := s.engines[name]; ok {
		engine.Cancel()
	}

	err := s.store.CancelExecution(name)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}

	exec, _ := s.store.GetExecution(name)
	return storeExecutionToProto(exec), nil
}

// --- Internal helpers ---

func (s *Server) runExecution(execName string, wfAST *ast.Workflow, args types.Value) {
	funcs := stdlib.NewRegistry()
	funcs.RegisterHTTP(&http.Client{Timeout: 30 * time.Second})

	engine := runtime.NewEngine(wfAST, funcs)
	s.engines[execName] = engine

	ctx := context.Background()
	result, err := engine.Execute(ctx, args)

	delete(s.engines, execName)

	if err != nil {
		_ = s.store.FailExecution(execName, err)
	} else {
		_ = s.store.CompleteExecution(execName, result)
	}
}

func storeWorkflowToProto(wf *store.Workflow) *workflowspb.Workflow {
	pb := &workflowspb.Workflow{
		Name:        wf.Name,
		Description: wf.Description,
		RevisionId:  wf.RevisionID,
		CreateTime:  timestamppb.New(wf.CreateTime),
		UpdateTime:  timestamppb.New(wf.UpdateTime),
	}

	switch wf.State {
	case store.WorkflowActive:
		pb.State = workflowspb.Workflow_ACTIVE
	default:
		pb.State = workflowspb.Workflow_STATE_UNSPECIFIED
	}

	if wf.SourceCode != "" {
		pb.SourceCode = &workflowspb.Workflow_SourceContents{
			SourceContents: wf.SourceCode,
		}
	}

	return pb
}

func storeExecutionToProto(exec *store.Execution) *executionspb.Execution {
	pb := &executionspb.Execution{
		Name:               exec.Name,
		StartTime:          timestamppb.New(exec.StartTime),
		Argument:           exec.Argument,
		Result:             exec.Result,
		WorkflowRevisionId: exec.WorkflowRevisionID,
	}

	switch exec.State {
	case store.ExecutionActive:
		pb.State = executionspb.Execution_ACTIVE
	case store.ExecutionSucceeded:
		pb.State = executionspb.Execution_SUCCEEDED
	case store.ExecutionFailed:
		pb.State = executionspb.Execution_FAILED
	case store.ExecutionCancelled:
		pb.State = executionspb.Execution_CANCELLED
	default:
		pb.State = executionspb.Execution_STATE_UNSPECIFIED
	}

	if exec.Error != nil {
		pb.Error = &executionspb.Execution_Error{
			Payload: exec.Error.Payload,
			Context: exec.Error.Context,
		}
	}

	if !exec.EndTime.IsZero() {
		pb.EndTime = timestamppb.New(exec.EndTime)
	}

	return pb
}

// --- Operations Service (for official client LRO support) ---

// GetOperation returns a completed operation. Since the emulator completes all
// operations synchronously, this is a no-op stub that returns NotFound.
func (s *Server) GetOperation(ctx context.Context, req *longrunningpb.GetOperationRequest) (*longrunningpb.Operation, error) {
	return nil, status.Errorf(codes.NotFound, "operation %q not found (emulator completes all operations immediately)", req.GetName())
}

// doneOperation wraps a proto message in an already-completed LRO Operation.
func doneOperation(name string, msg proto.Message) (*longrunningpb.Operation, error) {
	any, err := anypb.New(msg)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to marshal operation result: %v", err)
	}
	return &longrunningpb.Operation{
		Name: fmt.Sprintf("projects/-/locations/-/operations/%s", name),
		Done: true,
		Result: &longrunningpb.Operation_Response{
			Response: any,
		},
	}, nil
}
