// Package store provides in-memory storage for workflows and executions.
package store

import (
	"fmt"
	"sync"
	"time"

	"github.com/lemonberrylabs/gcw-emulator/pkg/types"
)

// WorkflowState represents the state of a stored workflow.
type WorkflowState string

const (
	WorkflowActive WorkflowState = "ACTIVE"
)

// ExecutionState represents the state of a workflow execution.
type ExecutionState string

const (
	ExecutionActive    ExecutionState = "ACTIVE"
	ExecutionSucceeded ExecutionState = "SUCCEEDED"
	ExecutionFailed    ExecutionState = "FAILED"
	ExecutionCancelled ExecutionState = "CANCELLED"
)

// Workflow represents a stored workflow definition.
type Workflow struct {
	Name        string        `json:"name"`
	Description string        `json:"description,omitempty"`
	State       WorkflowState `json:"state"`
	RevisionID  string        `json:"revisionId"`
	CreateTime  time.Time     `json:"createTime"`
	UpdateTime  time.Time     `json:"updateTime"`
	SourceCode  string        `json:"sourceContents"`
	Labels      map[string]string `json:"labels,omitempty"`
}

// Execution represents a stored workflow execution.
type Execution struct {
	Name       string         `json:"name"`
	State      ExecutionState `json:"state"`
	Argument   string         `json:"argument,omitempty"`
	Result     string         `json:"result,omitempty"`
	Error      *ExecutionError `json:"error,omitempty"`
	StartTime  time.Time      `json:"startTime"`
	EndTime    time.Time      `json:"endTime,omitempty"`
	WorkflowRevisionID string `json:"workflowRevisionId"`
}

// ExecutionError represents an error in a failed execution.
type ExecutionError struct {
	Payload string `json:"payload"`
	Context string `json:"context,omitempty"`
}

// Callback represents a callback endpoint for a waiting execution.
type Callback struct {
	Name         string    `json:"name"`
	Method       string    `json:"method"`
	URL          string    `json:"url"`
	ExecutionID  string    `json:"executionId"`
	CreateTime   time.Time `json:"createTime"`
}

// Store is a thread-safe in-memory storage for workflows and executions.
type Store struct {
	mu         sync.RWMutex
	workflows  map[string]*Workflow
	executions map[string]*Execution
	callbacks  map[string]*Callback

	// Counters for generating unique IDs
	execCounter int64
	revCounter  int64
}

// New creates a new empty store.
func New() *Store {
	return &Store{
		workflows:  make(map[string]*Workflow),
		executions: make(map[string]*Execution),
		callbacks:  make(map[string]*Callback),
	}
}

// CreateWorkflow creates a new workflow definition.
func (s *Store) CreateWorkflow(parent, workflowID, sourceCode, description string) (*Workflow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	name := fmt.Sprintf("%s/workflows/%s", parent, workflowID)
	if _, exists := s.workflows[name]; exists {
		return nil, fmt.Errorf("workflow '%s' already exists", name)
	}

	s.revCounter++
	now := time.Now()
	wf := &Workflow{
		Name:       name,
		Description: description,
		State:      WorkflowActive,
		RevisionID: fmt.Sprintf("%06d-000", s.revCounter),
		CreateTime: now,
		UpdateTime: now,
		SourceCode: sourceCode,
	}
	s.workflows[name] = wf
	return wf, nil
}

// GetWorkflow retrieves a workflow by its full name.
func (s *Store) GetWorkflow(name string) (*Workflow, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	wf, ok := s.workflows[name]
	if !ok {
		return nil, fmt.Errorf("workflow '%s' not found", name)
	}
	return wf, nil
}

// ListWorkflows returns all workflows under a parent.
func (s *Store) ListWorkflows(parent string) []*Workflow {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*Workflow
	prefix := parent + "/workflows/"
	for name, wf := range s.workflows {
		if len(name) > len(prefix) && name[:len(prefix)] == prefix {
			result = append(result, wf)
		}
	}
	return result
}

// UpdateWorkflow updates a workflow's source code.
func (s *Store) UpdateWorkflow(name, sourceCode, description string) (*Workflow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	wf, ok := s.workflows[name]
	if !ok {
		return nil, fmt.Errorf("workflow '%s' not found", name)
	}

	s.revCounter++
	wf.SourceCode = sourceCode
	if description != "" {
		wf.Description = description
	}
	wf.RevisionID = fmt.Sprintf("%06d-000", s.revCounter)
	wf.UpdateTime = time.Now()

	return wf, nil
}

// DeleteWorkflow removes a workflow.
func (s *Store) DeleteWorkflow(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.workflows[name]; !ok {
		return fmt.Errorf("workflow '%s' not found", name)
	}
	delete(s.workflows, name)
	return nil
}

// CreateExecution creates a new execution record.
func (s *Store) CreateExecution(workflowName string, argument types.Value) (*Execution, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	wf, ok := s.workflows[workflowName]
	if !ok {
		return nil, fmt.Errorf("workflow '%s' not found", workflowName)
	}

	s.execCounter++
	execID := fmt.Sprintf("exec-%d", s.execCounter)
	name := fmt.Sprintf("%s/executions/%s", workflowName, execID)

	var argStr string
	if !argument.IsNull() {
		b, _ := argument.MarshalJSON()
		argStr = string(b)
	}

	exec := &Execution{
		Name:              name,
		State:             ExecutionActive,
		Argument:          argStr,
		StartTime:         time.Now(),
		WorkflowRevisionID: wf.RevisionID,
	}
	s.executions[name] = exec
	return exec, nil
}

// GetExecution retrieves an execution by name.
func (s *Store) GetExecution(name string) (*Execution, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	exec, ok := s.executions[name]
	if !ok {
		return nil, fmt.Errorf("execution '%s' not found", name)
	}
	return exec, nil
}

// ListExecutions returns all executions for a workflow.
func (s *Store) ListExecutions(workflowName string) []*Execution {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*Execution
	prefix := workflowName + "/executions/"
	for name, exec := range s.executions {
		if len(name) > len(prefix) && name[:len(prefix)] == prefix {
			result = append(result, exec)
		}
	}
	return result
}

// CompleteExecution marks an execution as succeeded with a result.
func (s *Store) CompleteExecution(name string, result types.Value) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	exec, ok := s.executions[name]
	if !ok {
		return fmt.Errorf("execution '%s' not found", name)
	}

	exec.State = ExecutionSucceeded
	exec.EndTime = time.Now()

	b, _ := result.MarshalJSON()
	exec.Result = string(b)

	return nil
}

// FailExecution marks an execution as failed with an error.
func (s *Store) FailExecution(name string, err error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	exec, ok := s.executions[name]
	if !ok {
		return fmt.Errorf("execution '%s' not found", name)
	}

	exec.State = ExecutionFailed
	exec.EndTime = time.Now()

	payload := err.Error()
	if we, ok := err.(*types.WorkflowError); ok {
		b, _ := we.ToValue().MarshalJSON()
		payload = string(b)
	}
	exec.Error = &ExecutionError{Payload: payload}

	return nil
}

// CancelExecution marks an execution as cancelled.
func (s *Store) CancelExecution(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	exec, ok := s.executions[name]
	if !ok {
		return fmt.Errorf("execution '%s' not found", name)
	}

	if exec.State != ExecutionActive {
		return fmt.Errorf("execution '%s' is not active (state: %s)", name, exec.State)
	}

	exec.State = ExecutionCancelled
	exec.EndTime = time.Now()
	return nil
}

// CreateCallback stores a callback endpoint.
func (s *Store) CreateCallback(executionID, method, callbackURL string) *Callback {
	s.mu.Lock()
	defer s.mu.Unlock()

	cb := &Callback{
		Name:        fmt.Sprintf("callback-%d", len(s.callbacks)+1),
		Method:      method,
		URL:         callbackURL,
		ExecutionID: executionID,
		CreateTime:  time.Now(),
	}
	s.callbacks[cb.URL] = cb
	return cb
}

// GetCallback retrieves a callback by its URL.
func (s *Store) GetCallback(callbackURL string) (*Callback, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cb, ok := s.callbacks[callbackURL]
	if !ok {
		return nil, fmt.Errorf("callback not found")
	}
	return cb, nil
}

// ListCallbacks returns all callbacks for an execution.
func (s *Store) ListCallbacks(executionName string) []*Callback {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*Callback
	for _, cb := range s.callbacks {
		if cb.ExecutionID == executionName {
			result = append(result, cb)
		}
	}
	return result
}
