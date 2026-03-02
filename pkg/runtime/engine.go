package runtime

import (
	"context"
	"fmt"
	"sync"

	"github.com/lemonberrylabs/gcw-emulator/pkg/ast"
	"github.com/lemonberrylabs/gcw-emulator/pkg/types"
)

// MaxCallStackDepth is the maximum allowed call stack depth for subworkflows.
const MaxCallStackDepth = 20

// MaxStepsPerExecution is the maximum number of steps that can execute in a single run.
const MaxStepsPerExecution = 100_000

// FlowControl represents special flow control signals during execution.
type FlowControl int

const (
	FlowNone     FlowControl = iota
	FlowNext                         // jump to a named step
	FlowEnd                          // end the current workflow
	FlowReturn                       // return a value
	FlowBreak                        // break out of a loop
	FlowContinue                     // continue to next iteration
)

// StepResult is the result of executing a single step.
type StepResult struct {
	Flow      FlowControl
	NextStep  string      // step name for FlowNext
	Value     types.Value // return value for FlowReturn
}

// Engine executes GCW workflows.
type Engine struct {
	workflow *ast.Workflow
	funcs    FunctionRegistry

	mu        sync.Mutex
	stepCount int
	callDepth int
	cancelled bool
}

// contextKey is an unexported type for context keys defined in this package.
type contextKey string

// parallelDepthKey is the context key for tracking parallel nesting depth
// per execution path. Using context (rather than a shared counter on Engine)
// ensures that sibling goroutines spawned by the same parallel step each
// see the correct structural nesting depth.
const parallelDepthKey contextKey = "parallelDepth"

// parallelDepthFromCtx returns the current parallel nesting depth stored
// in ctx. Returns 0 when no parallel step is active.
func parallelDepthFromCtx(ctx context.Context) int {
	if v, ok := ctx.Value(parallelDepthKey).(int); ok {
		return v
	}
	return 0
}

// NewEngine creates a new workflow execution engine.
func NewEngine(workflow *ast.Workflow, funcs FunctionRegistry) *Engine {
	return &Engine{
		workflow: workflow,
		funcs:    funcs,
	}
}

// Execute runs the main workflow with the given arguments and returns the result.
func (e *Engine) Execute(ctx context.Context, args types.Value) (types.Value, error) {
	scope := NewScope()

	// Set up main workflow parameters
	// In GCW, when main has a single param, the entire argument map is
	// assigned to that param. When main has multiple params, each param
	// is extracted from the argument map by name.
	if len(e.workflow.Main.Params) > 0 && !args.IsNull() {
		if len(e.workflow.Main.Params) == 1 {
			// Single param: assign entire args to it
			scope.Set(e.workflow.Main.Params[0].Name, args)
		} else {
			// Multiple params: extract from args map
			argsMap := args.AsMap()
			if argsMap != nil {
				for _, param := range e.workflow.Main.Params {
					if v, ok := argsMap.Get(param.Name); ok {
						scope.Set(param.Name, v)
					} else if param.HasDefault {
						scope.Set(param.Name, types.Null)
					}
				}
			}
		}
	}

	// Execute main directly without counting toward call stack depth
	result, err := e.executeSteps(ctx, e.workflow.Main.Steps, scope)
	if err != nil {
		return types.Null, err
	}
	return result.Value, nil
}

// executeSubworkflow runs a subworkflow with its own scope.
func (e *Engine) executeSubworkflow(ctx context.Context, sub *ast.Subworkflow, scope *VariableScope) (types.Value, error) {
	e.mu.Lock()
	e.callDepth++
	depth := e.callDepth
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		e.callDepth--
		e.mu.Unlock()
	}()

	if depth > MaxCallStackDepth {
		return types.Null, types.NewRecursionError()
	}

	result, err := e.executeSteps(ctx, sub.Steps, scope)
	if err != nil {
		return types.Null, err
	}
	return result.Value, nil
}

// executeSteps runs a sequence of steps and returns the result.
func (e *Engine) executeSteps(ctx context.Context, steps []*ast.Step, scope *VariableScope) (StepResult, error) {
	if len(steps) == 0 {
		return StepResult{Flow: FlowEnd, Value: types.Null}, nil
	}

	// Build step index for next jumps
	stepIndex := make(map[string]int)
	for i, step := range steps {
		stepIndex[step.Name] = i
	}

	i := 0
	for i < len(steps) {
		select {
		case <-ctx.Done():
			return StepResult{}, ctx.Err()
		default:
		}

		e.mu.Lock()
		if e.cancelled {
			e.mu.Unlock()
			return StepResult{}, fmt.Errorf("execution cancelled")
		}
		e.stepCount++
		if e.stepCount > MaxStepsPerExecution {
			e.mu.Unlock()
			return StepResult{}, types.NewResourceLimitError(
				fmt.Sprintf("execution exceeded maximum step limit of %d", MaxStepsPerExecution))
		}
		e.mu.Unlock()

		step := steps[i]
		result, err := e.executeStep(ctx, step, scope)
		if err != nil {
			return StepResult{}, err
		}

		switch result.Flow {
		case FlowNone:
			i++
		case FlowNext:
			if result.NextStep == "end" {
				return StepResult{Flow: FlowEnd, Value: types.Null}, nil
			}
			if result.NextStep == "break" {
				return StepResult{Flow: FlowBreak}, nil
			}
			if result.NextStep == "continue" {
				return StepResult{Flow: FlowContinue}, nil
			}
			idx, ok := stepIndex[result.NextStep]
			if !ok {
				return StepResult{}, fmt.Errorf("step '%s' not found", result.NextStep)
			}
			i = idx
		case FlowEnd:
			return result, nil
		case FlowReturn:
			return result, nil
		case FlowBreak:
			return result, nil
		case FlowContinue:
			return result, nil
		}
	}

	return StepResult{Flow: FlowNone}, nil
}

// executeStep runs a single step.
func (e *Engine) executeStep(ctx context.Context, step *ast.Step, scope *VariableScope) (StepResult, error) {
	var result StepResult
	var err error

	// Handle nested steps grouping
	if step.Steps != nil {
		result, err = e.executeSteps(ctx, step.Steps, scope)
		if err != nil {
			return StepResult{}, err
		}
		if result.Flow == FlowReturn || result.Flow == FlowBreak || result.Flow == FlowContinue {
			return result, nil
		}
	}

	// Handle assign step
	if step.Assign != nil {
		err = e.executeAssign(step.Assign, scope)
		if err != nil {
			return StepResult{}, err
		}
	}

	// Handle call step
	if step.Call != nil {
		err = e.executeCall(ctx, step.Call, scope)
		if err != nil {
			return StepResult{}, err
		}
	}

	// Handle switch step
	if step.Switch != nil {
		result, err = e.executeSwitch(ctx, step.Switch, scope)
		if err != nil {
			return StepResult{}, err
		}
		if result.Flow != FlowNone {
			return result, nil
		}
	}

	// Handle for loop
	if step.For != nil {
		result, err = e.executeFor(ctx, step.For, scope)
		if err != nil {
			return StepResult{}, err
		}
		if result.Flow == FlowReturn || result.Flow == FlowEnd {
			return result, nil
		}
	}

	// Handle try/except/retry
	if step.Try != nil {
		result, err = e.executeTry(ctx, step.Try, scope)
		if err != nil {
			return StepResult{}, err
		}
		if result.Flow != FlowNone {
			return result, nil
		}
	}

	// Handle parallel
	if step.Parallel != nil {
		err = e.executeParallel(ctx, step.Parallel, scope)
		if err != nil {
			return StepResult{}, err
		}
	}

	// Handle raise
	if step.Raise != nil {
		return StepResult{}, e.executeRaise(step.Raise, scope)
	}

	// Handle return
	if step.HasReturn {
		val, err := EvalValue(step.Return, scope, e.funcs)
		if err != nil {
			return StepResult{}, err
		}
		return StepResult{Flow: FlowReturn, Value: val}, nil
	}

	// Handle next
	if step.Next != "" {
		return StepResult{Flow: FlowNext, NextStep: step.Next}, nil
	}

	return result, nil
}

// MaxAssignments is the maximum number of assignments per assign step.
const MaxAssignments = 50

// executeAssign executes an assign step.
func (e *Engine) executeAssign(assignments []ast.Assignment, scope *VariableScope) error {
	if len(assignments) > MaxAssignments {
		return types.NewResourceLimitError(
			fmt.Sprintf("assign step exceeds maximum of %d assignments", MaxAssignments))
	}
	// Lock shared mutex for atomic read-eval-write in parallel contexts
	scope.LockShared()
	defer scope.UnlockShared()

	for _, a := range assignments {
		val, err := EvalValue(a.Value, scope, e.funcs)
		if err != nil {
			return err
		}
		err = SetByPath(scope, a.Target, val)
		if err != nil {
			return err
		}
	}
	return nil
}

// executeCall executes a call step.
func (e *Engine) executeCall(ctx context.Context, call *ast.CallExpr, scope *VariableScope) error {
	// Check if it's a subworkflow call
	if sub, ok := e.workflow.Subworkflows[call.Function]; ok {
		return e.executeSubworkflowCall(ctx, sub, call, scope)
	}

	// It's a stdlib/HTTP call — evaluate args and call through the function registry
	args := make([]types.Value, 0)

	if call.Args != nil {
		// Build a single map argument for function calls
		argMap := types.NewOrderedMap()
		for k, v := range call.Args {
			val, err := EvalValue(v, scope, e.funcs)
			if err != nil {
				return err
			}
			argMap.Set(k, val)
		}
		args = append(args, types.NewMap(argMap))
	}

	result, err := e.funcs.CallFunction(call.Function, args)
	if err != nil {
		return err
	}

	if call.Result != "" {
		scope.Set(call.Result, result)
	}

	return nil
}

// executeSubworkflowCall calls a subworkflow with evaluated arguments.
func (e *Engine) executeSubworkflowCall(ctx context.Context, sub *ast.Subworkflow, call *ast.CallExpr, parentScope *VariableScope) error {
	childScope := NewScope()

	// Evaluate and set parameters
	for _, param := range sub.Params {
		if call.Args != nil {
			if argExpr, ok := call.Args[param.Name]; ok {
				val, err := EvalValue(argExpr, parentScope, e.funcs)
				if err != nil {
					return err
				}
				childScope.Set(param.Name, val)
				continue
			}
		}
		// Use default value
		if param.HasDefault {
			val, err := EvalValue(param.Default, parentScope, e.funcs)
			if err != nil {
				return err
			}
			childScope.Set(param.Name, val)
		} else {
			return fmt.Errorf("missing required argument '%s' for subworkflow '%s'", param.Name, sub.Name)
		}
	}

	result, err := e.executeSubworkflow(ctx, sub, childScope)
	if err != nil {
		return err
	}

	if call.Result != "" {
		parentScope.Set(call.Result, result)
	}

	return nil
}

// MaxSwitchConditions is the maximum number of conditions per switch step.
const MaxSwitchConditions = 50

// executeSwitch executes a switch step.
func (e *Engine) executeSwitch(ctx context.Context, conditions []ast.SwitchCondition, scope *VariableScope) (StepResult, error) {
	if len(conditions) > MaxSwitchConditions {
		return StepResult{}, types.NewResourceLimitError(
			fmt.Sprintf("switch step exceeds maximum of %d conditions", MaxSwitchConditions))
	}
	for _, cond := range conditions {
		if cond.Condition != nil {
			val, err := EvalValue(cond.Condition, scope, e.funcs)
			if err != nil {
				return StepResult{}, err
			}
			if !val.Truthy() {
				continue
			}
		}

		// Condition matched - execute any inline actions
		if cond.Assign != nil {
			err := e.executeAssign(cond.Assign, scope)
			if err != nil {
				return StepResult{}, err
			}
		}

		if cond.Steps != nil {
			result, err := e.executeSteps(ctx, cond.Steps, scope)
			if err != nil {
				return StepResult{}, err
			}
			if result.Flow != FlowNone {
				return result, nil
			}
		}

		if cond.HasReturn {
			val, err := EvalValue(cond.Return, scope, e.funcs)
			if err != nil {
				return StepResult{}, err
			}
			return StepResult{Flow: FlowReturn, Value: val}, nil
		}

		if cond.Raise != nil {
			return StepResult{}, e.executeRaise(cond.Raise, scope)
		}

		if cond.Next != "" {
			return StepResult{Flow: FlowNext, NextStep: cond.Next}, nil
		}

		// First matching condition wins, return none to continue normal flow
		return StepResult{}, nil
	}

	// No condition matched — continue to next step
	return StepResult{}, nil
}

// executeFor executes a for loop.
func (e *Engine) executeFor(ctx context.Context, forExpr *ast.ForExpr, parentScope *VariableScope) (StepResult, error) {
	var items []types.Value
	var indices []types.Value

	if forExpr.HasRange {
		// Evaluate range bounds
		startVal, err := EvalValue(forExpr.Range[0], parentScope, e.funcs)
		if err != nil {
			return StepResult{}, err
		}
		endVal, err := EvalValue(forExpr.Range[1], parentScope, e.funcs)
		if err != nil {
			return StepResult{}, err
		}
		start := startVal.AsInt()
		end := endVal.AsInt()
		for i := start; i <= end; i++ {
			items = append(items, types.NewInt(i))
		}
	} else {
		// Evaluate the iterable
		iterVal, err := EvalValue(forExpr.In, parentScope, e.funcs)
		if err != nil {
			return StepResult{}, err
		}

		switch iterVal.Type() {
		case types.TypeList:
			items = iterVal.AsList()
		case types.TypeMap:
			// Iterate over keys
			for _, k := range iterVal.AsMap().Keys() {
				items = append(items, types.NewString(k))
			}
		default:
			return StepResult{}, types.NewTypeError(
				fmt.Sprintf("cannot iterate over %s", iterVal.Type()))
		}
	}

	// Generate indices
	for i := range items {
		indices = append(indices, types.NewInt(int64(i)))
	}

	for i, item := range items {
		// Create loop scope
		loopScope := parentScope.NewChildScope()
		loopScope.SetLocal(forExpr.Value, item)
		if forExpr.Index != "" {
			loopScope.SetLocal(forExpr.Index, indices[i])
		}

		result, err := e.executeSteps(ctx, forExpr.Steps, loopScope)
		if err != nil {
			return StepResult{}, err
		}

		switch result.Flow {
		case FlowBreak:
			return StepResult{}, nil // break exits the loop
		case FlowContinue:
			continue // continue to next iteration
		case FlowReturn:
			return result, nil // return propagates up
		case FlowEnd:
			return result, nil
		}
	}

	return StepResult{}, nil
}

// executeTry executes a try/except/retry step.
func (e *Engine) executeTry(ctx context.Context, tryExpr *ast.TryExpr, scope *VariableScope) (StepResult, error) {
	maxAttempts := 1
	if tryExpr.Retry != nil {
		maxAttempts = tryExpr.Retry.MaxRetries + 1
	}

	var lastErr error

	for attempt := 0; attempt < maxAttempts; attempt++ {
		result, err := e.executeSteps(ctx, tryExpr.Try, scope)
		if err == nil {
			return result, nil
		}

		lastErr = err

		// Check if we should retry
		if tryExpr.Retry != nil && attempt < maxAttempts-1 {
			if e.shouldRetry(tryExpr.Retry, err, scope) {
				// Apply backoff delay if configured
				if tryExpr.Retry.Backoff != nil {
					delay := e.calculateBackoff(tryExpr.Retry.Backoff, attempt)
					_ = delay // TODO: actually sleep for delay seconds
				}
				continue
			}
		}
		break
	}

	// Error occurred — handle with except block if present
	if tryExpr.Except != nil {
		return e.executeExcept(ctx, tryExpr.Except, lastErr, scope)
	}

	return StepResult{}, lastErr
}

// shouldRetry determines if an error should be retried based on the retry predicate.
func (e *Engine) shouldRetry(retry *ast.RetryExpr, err error, scope *VariableScope) bool {
	if retry.Predicate == nil {
		return true // retry all if no predicate
	}

	// Check for built-in retry predicates
	if predStr, ok := retry.Predicate.(string); ok {
		// Handle ${http.default_retry} and similar
		predStr = extractExprString(predStr)
		switch predStr {
		case "http.default_retry", "http.default_retry_predicate":
			return isRetryableHTTPError(err)
		case "http.default_retry_non_idempotent":
			return isRetryableHTTPError(err)
		case "retry.always":
			return true
		case "retry.never":
			return false
		}
	}

	return true
}

// extractExprString extracts the expression from a ${...} wrapper.
func extractExprString(s string) string {
	if len(s) > 3 && s[:2] == "${" && s[len(s)-1] == '}' {
		return s[2 : len(s)-1]
	}
	return s
}

// isRetryableHTTPError checks if an error matches the HTTP default retry predicate.
func isRetryableHTTPError(err error) bool {
	we, ok := err.(*types.WorkflowError)
	if !ok {
		return false
	}
	if we.HasTag(types.TagConnectionError) || we.HasTag(types.TagTimeoutError) {
		return true
	}
	if we.HasTag(types.TagHttpError) {
		switch we.Code {
		case 429, 502, 503, 504:
			return true
		}
	}
	return false
}

// calculateBackoff calculates the delay for a retry attempt using exponential backoff.
func (e *Engine) calculateBackoff(backoff *ast.BackoffExpr, attempt int) float64 {
	delay := backoff.InitialDelay
	for i := 0; i < attempt; i++ {
		delay *= backoff.Multiplier
	}
	if delay > backoff.MaxDelay {
		delay = backoff.MaxDelay
	}
	return delay
}

// executeExcept handles an error in the except block.
func (e *Engine) executeExcept(ctx context.Context, except *ast.ExceptExpr, err error, scope *VariableScope) (StepResult, error) {
	// In GCW, except blocks share the parent scope so variables set inside
	// are visible after the try/except completes.
	// We bind the error variable directly in the current scope.
	if except.As != "" {
		var errVal types.Value
		if we, ok := err.(*types.WorkflowError); ok {
			errVal = we.ToValue()
		} else {
			// For non-WorkflowError errors, create a map with message field
			m := types.NewOrderedMap()
			m.Set("message", types.NewString(err.Error()))
			m.Set("code", types.NewInt(0))
			m.Set("tags", types.NewList(nil))
			errVal = types.NewMap(m)
		}
		scope.Set(except.As, errVal)
	}

	return e.executeSteps(ctx, except.Steps, scope)
}

// executeRaise raises an error from a raise step.
func (e *Engine) executeRaise(raiseExpr interface{}, scope *VariableScope) error {
	val, err := EvalValue(raiseExpr, scope, e.funcs)
	if err != nil {
		return err
	}

	switch val.Type() {
	case types.TypeString:
		return &types.WorkflowError{Message: val.AsString()}
	case types.TypeMap:
		we := types.ErrorFromValue(val)
		if we != nil {
			return we
		}
		return &types.WorkflowError{Message: val.String()}
	default:
		return &types.WorkflowError{Message: val.String()}
	}
}

// MaxParallelNestingDepth is the maximum allowed nesting depth for parallel steps.
const MaxParallelNestingDepth = 2

// executeParallel executes a parallel step.
func (e *Engine) executeParallel(ctx context.Context, p *ast.ParallelExpr, scope *VariableScope) error {
	depth := parallelDepthFromCtx(ctx) + 1
	if depth > MaxParallelNestingDepth {
		return types.NewParallelNestingError(
			fmt.Sprintf("parallel nesting depth %d exceeds maximum of %d", depth, MaxParallelNestingDepth))
	}

	// Propagate the incremented depth to child goroutines via context.
	ctx = context.WithValue(ctx, parallelDepthKey, depth)

	if p.Branches != nil {
		return e.executeParallelBranches(ctx, p, scope)
	}
	if p.For != nil {
		return e.executeParallelFor(ctx, p, scope)
	}
	return nil
}

// MaxParallelBranches is the maximum number of branches per parallel step.
const MaxParallelBranches = 10

// executeParallelBranches runs parallel branches using goroutines.
func (e *Engine) executeParallelBranches(ctx context.Context, p *ast.ParallelExpr, scope *VariableScope) error {
	if len(p.Branches) > MaxParallelBranches {
		return types.NewResourceLimitError(
			fmt.Sprintf("parallel step exceeds maximum of %d branches", MaxParallelBranches))
	}
	type branchResult struct {
		err error
	}

	results := make([]branchResult, len(p.Branches))
	var wg sync.WaitGroup

	// Determine concurrency limit
	limit := p.ConcurrencyLimit
	if limit <= 0 {
		limit = 20
	}
	sem := make(chan struct{}, limit)

	branchCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var mu sync.Mutex
	var firstErr error

	// Create shared mutex for all branches in this parallel step
	sharedMu := &sync.Mutex{}

	for i, branch := range p.Branches {
		wg.Add(1)
		go func(idx int, b *ast.ParallelBranch) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			// Create branch scope with shared variable access
			branchScope := &VariableScope{
				parent:   scope,
				vars:     make(map[string]types.Value),
				sharedMu: sharedMu,
			}

			_, err := e.executeSteps(branchCtx, b.Steps, branchScope)
			results[idx] = branchResult{err: err}

			if err != nil && p.ExceptionPolicy != "continueAll" {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
					cancel()
				}
				mu.Unlock()
			}
		}(i, branch)
	}

	wg.Wait()

	if firstErr != nil {
		return firstErr
	}

	// Check for errors in continueAll mode
	if p.ExceptionPolicy == "continueAll" {
		var branchErrors []error
		for _, r := range results {
			if r.err != nil {
				branchErrors = append(branchErrors, r.err)
			}
		}
		if len(branchErrors) > 0 {
			return types.NewUnhandledBranchError(branchErrors[0].Error())
		}
	}

	return nil
}

// executeParallelFor runs a parallel for loop.
func (e *Engine) executeParallelFor(ctx context.Context, p *ast.ParallelExpr, scope *VariableScope) error {
	// Evaluate the iterable
	iterVal, err := EvalValue(p.For.In, scope, e.funcs)
	if err != nil {
		return err
	}

	var items []types.Value
	switch iterVal.Type() {
	case types.TypeList:
		items = iterVal.AsList()
	default:
		return types.NewTypeError(fmt.Sprintf("cannot iterate over %s in parallel for", iterVal.Type()))
	}

	limit := p.ConcurrencyLimit
	if limit <= 0 {
		limit = 20
	}
	sem := make(chan struct{}, limit)

	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	forCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Create shared mutex for all iterations in this parallel for
	sharedMu := &sync.Mutex{}

	for i, item := range items {
		wg.Add(1)
		go func(idx int, it types.Value) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			iterScope := &VariableScope{
				parent:   scope,
				vars:     make(map[string]types.Value),
				sharedMu: sharedMu,
			}
			iterScope.SetLocal(p.For.Value, it)
			if p.For.Index != "" {
				iterScope.SetLocal(p.For.Index, types.NewInt(int64(idx)))
			}

			_, err := e.executeSteps(forCtx, p.For.Steps, iterScope)
			if err != nil && p.ExceptionPolicy != "continueAll" {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
					cancel()
				}
				mu.Unlock()
			}
		}(i, item)
	}

	wg.Wait()
	return firstErr
}

// Cancel cancels the current execution.
func (e *Engine) Cancel() {
	e.mu.Lock()
	e.cancelled = true
	e.mu.Unlock()
}

// StepCount returns the current step count.
func (e *Engine) StepCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.stepCount
}
