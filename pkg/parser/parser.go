// Package parser converts YAML/JSON workflow definitions into AST types.
package parser

import (
	"fmt"
	"strings"

	"github.com/lemonberrylabs/gcw-emulator/pkg/ast"
	"gopkg.in/yaml.v3"
)

// preprocessSource quotes ${{ ... }} map literal expressions so the YAML parser
// doesn't interpret them as flow mappings. In GCW, ${{ ... }} is a map literal
// expression, not a YAML flow mapping.
func preprocessSource(source []byte) []byte {
	s := string(source)
	if !strings.Contains(s, "${{") {
		return source
	}

	var result strings.Builder
	for _, line := range strings.Split(s, "\n") {
		idx := strings.Index(line, "${{")
		if idx >= 0 {
			// Find the matching closing }} accounting for strings
			rest := line[idx:]
			depth := 0
			inStr := false
			strChar := byte(0)
			end := -1
			for i := 0; i < len(rest); i++ {
				ch := rest[i]
				if inStr {
					if ch == '\\' && i+1 < len(rest) {
						i++ // skip escaped char
						continue
					}
					if ch == strChar {
						inStr = false
					}
					continue
				}
				if ch == '"' || ch == '\'' {
					inStr = true
					strChar = ch
					continue
				}
				if ch == '{' {
					depth++
				} else if ch == '}' {
					depth--
					if depth == 0 {
						end = i + 1
						break
					}
				}
			}
			if end > 0 {
				expr := rest[:end]
				prefix := line[:idx]
				suffix := line[idx+end:]
				result.WriteString(prefix + "'" + expr + "'" + suffix)
			} else {
				result.WriteString(line)
			}
		} else {
			result.WriteString(line)
		}
		result.WriteByte('\n')
	}
	return []byte(strings.TrimRight(result.String(), "\n"))
}

// MaxAssignments is the maximum number of assignments per assign step.
const MaxAssignments = 50

// MaxConditions is the maximum number of conditions per switch step.
const MaxConditions = 50

// MaxBranches is the maximum number of branches per parallel step.
const MaxBranches = 10

// MaxSourceSize is the maximum workflow source code size in bytes (128 KB).
const MaxSourceSize = 128 * 1024

// ParseError represents an error encountered during workflow parsing.
type ParseError struct {
	Message  string
	Location string // e.g., "step 'foo' in workflow 'main'"
}

func (e *ParseError) Error() string {
	if e.Location != "" {
		return fmt.Sprintf("parse error at %s: %s", e.Location, e.Message)
	}
	return fmt.Sprintf("parse error: %s", e.Message)
}

// Parse parses a YAML or JSON workflow definition into an AST Workflow.
func Parse(source []byte) (*ast.Workflow, error) {
	if len(source) > MaxSourceSize {
		return nil, &ParseError{Message: fmt.Sprintf("workflow source size %d exceeds maximum %d bytes", len(source), MaxSourceSize)}
	}

	// Preprocess ${{ }} map literal syntax before YAML parsing
	source = preprocessSource(source)

	// Parse YAML into a generic map
	var raw yaml.Node
	if err := yaml.Unmarshal(source, &raw); err != nil {
		return nil, &ParseError{Message: fmt.Sprintf("invalid YAML: %v", err)}
	}

	// The root node is a document node containing the actual content
	if raw.Kind != yaml.DocumentNode || len(raw.Content) == 0 {
		return nil, &ParseError{Message: "empty workflow definition"}
	}

	rootNode := raw.Content[0]
	if rootNode.Kind != yaml.MappingNode {
		return nil, &ParseError{Message: "workflow definition must be a mapping"}
	}

	workflow := &ast.Workflow{
		Subworkflows: make(map[string]*ast.Subworkflow),
	}

	// Parse each top-level key as a workflow/subworkflow name
	for i := 0; i+1 < len(rootNode.Content); i += 2 {
		nameNode := rootNode.Content[i]
		bodyNode := rootNode.Content[i+1]

		name := nameNode.Value
		sub, err := parseSubworkflow(name, bodyNode)
		if err != nil {
			return nil, err
		}

		if name == "main" {
			workflow.Main = sub
		} else {
			workflow.Subworkflows[name] = sub
		}
	}

	if workflow.Main == nil {
		return nil, &ParseError{Message: "workflow must have a 'main' workflow"}
	}

	return workflow, nil
}

// parseSubworkflow parses a single workflow/subworkflow body.
func parseSubworkflow(name string, node *yaml.Node) (*ast.Subworkflow, error) {
	sub := &ast.Subworkflow{Name: name}

	if node.Kind != yaml.MappingNode {
		// If it's a sequence, treat it as steps directly (shorthand for main without params)
		if node.Kind == yaml.SequenceNode {
			steps, err := parseSteps(node, name)
			if err != nil {
				return nil, err
			}
			sub.Steps = steps
			return sub, nil
		}
		return nil, &ParseError{
			Message:  "workflow body must be a mapping or sequence",
			Location: fmt.Sprintf("workflow '%s'", name),
		}
	}

	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		val := node.Content[i+1]

		switch key {
		case "params":
			params, err := parseParams(val, name)
			if err != nil {
				return nil, err
			}
			sub.Params = params
		case "steps":
			steps, err := parseSteps(val, name)
			if err != nil {
				return nil, err
			}
			sub.Steps = steps
		default:
			return nil, &ParseError{
				Message:  fmt.Sprintf("unknown key '%s' in workflow body", key),
				Location: fmt.Sprintf("workflow '%s'", name),
			}
		}
	}

	if sub.Steps == nil {
		return nil, &ParseError{
			Message:  "workflow must have 'steps'",
			Location: fmt.Sprintf("workflow '%s'", name),
		}
	}

	return sub, nil
}

// parseParams parses workflow parameter definitions.
func parseParams(node *yaml.Node, workflowName string) ([]ast.Param, error) {
	if node.Kind != yaml.SequenceNode {
		return nil, &ParseError{
			Message:  "params must be a sequence",
			Location: fmt.Sprintf("workflow '%s'", workflowName),
		}
	}

	var params []ast.Param
	for _, item := range node.Content {
		switch item.Kind {
		case yaml.ScalarNode:
			// Simple parameter: just a name
			params = append(params, ast.Param{Name: item.Value})
		case yaml.MappingNode:
			// Parameter with default: {name: default_value}
			if len(item.Content) != 2 {
				return nil, &ParseError{
					Message:  "parameter with default must be a single key-value pair",
					Location: fmt.Sprintf("params in workflow '%s'", workflowName),
				}
			}
			name := item.Content[0].Value
			defaultVal := nodeToInterface(item.Content[1])
			params = append(params, ast.Param{
				Name:       name,
				Default:    defaultVal,
				HasDefault: true,
			})
		default:
			return nil, &ParseError{
				Message:  "invalid parameter definition",
				Location: fmt.Sprintf("params in workflow '%s'", workflowName),
			}
		}
	}
	return params, nil
}

// parseSteps parses a sequence of step definitions.
func parseSteps(node *yaml.Node, context string) ([]*ast.Step, error) {
	if node.Kind != yaml.SequenceNode {
		return nil, &ParseError{
			Message:  "steps must be a sequence",
			Location: context,
		}
	}

	var steps []*ast.Step
	for _, item := range node.Content {
		if item.Kind != yaml.MappingNode || len(item.Content) != 2 {
			return nil, &ParseError{
				Message:  "each step must be a single-key mapping",
				Location: context,
			}
		}

		stepName := item.Content[0].Value
		stepBody := item.Content[1]

		step, err := parseStep(stepName, stepBody, context)
		if err != nil {
			return nil, err
		}
		steps = append(steps, step)
	}

	return steps, nil
}

// parseStep parses a single step body.
func parseStep(name string, body *yaml.Node, context string) (*ast.Step, error) {
	step := &ast.Step{Name: name}
	loc := fmt.Sprintf("step '%s' in %s", name, context)

	if body.Kind != yaml.MappingNode {
		return nil, &ParseError{
			Message:  "step body must be a mapping",
			Location: loc,
		}
	}

	for i := 0; i+1 < len(body.Content); i += 2 {
		key := body.Content[i].Value
		val := body.Content[i+1]

		switch key {
		case "assign":
			assignments, err := parseAssignments(val, loc)
			if err != nil {
				return nil, err
			}
			step.Assign = assignments

		case "call":
			if step.Call == nil {
				step.Call = &ast.CallExpr{}
			}
			step.Call.Function = val.Value

		case "args":
			if step.Call == nil {
				step.Call = &ast.CallExpr{}
			}
			args, err := parseCallArgs(val, loc)
			if err != nil {
				return nil, err
			}
			step.Call.Args = args

		case "result":
			if step.Call != nil {
				step.Call.Result = val.Value
			}
			step.Result = val.Value

		case "switch":
			conditions, err := parseSwitchConditions(val, loc)
			if err != nil {
				return nil, err
			}
			step.Switch = conditions

		case "for":
			forExpr, err := parseFor(val, loc)
			if err != nil {
				return nil, err
			}
			step.For = forExpr

		case "parallel":
			parallel, err := parseParallel(val, loc)
			if err != nil {
				return nil, err
			}
			step.Parallel = parallel

		case "try":
			if step.Try == nil {
				step.Try = &ast.TryExpr{}
			}
			trySteps, err := parseTrySteps(val, loc)
			if err != nil {
				return nil, err
			}
			step.Try.Try = trySteps

		case "except":
			if step.Try == nil {
				step.Try = &ast.TryExpr{}
			}
			except, err := parseExcept(val, loc)
			if err != nil {
				return nil, err
			}
			step.Try.Except = except

		case "retry":
			if step.Try == nil {
				step.Try = &ast.TryExpr{}
			}
			retry, err := parseRetry(val, loc)
			if err != nil {
				return nil, err
			}
			step.Try.Retry = retry

		case "raise":
			step.Raise = nodeToInterface(val)

		case "return":
			step.Return = nodeToInterface(val)
			step.HasReturn = true

		case "next":
			step.Next = val.Value

		case "steps":
			nestedSteps, err := parseSteps(val, loc)
			if err != nil {
				return nil, err
			}
			step.Steps = nestedSteps

		default:
			return nil, &ParseError{
				Message:  fmt.Sprintf("unknown step key '%s'", key),
				Location: loc,
			}
		}
	}

	return step, nil
}

// parseAssignments parses the value of an assign step.
func parseAssignments(node *yaml.Node, loc string) ([]ast.Assignment, error) {
	if node.Kind != yaml.SequenceNode {
		return nil, &ParseError{
			Message:  "assign must be a sequence",
			Location: loc,
		}
	}

	var assignments []ast.Assignment
	for _, item := range node.Content {
		if item.Kind != yaml.MappingNode || len(item.Content) != 2 {
			return nil, &ParseError{
				Message:  "each assignment must be a single key-value pair",
				Location: loc,
			}
		}
		target := item.Content[0].Value
		value := nodeToInterface(item.Content[1])
		assignments = append(assignments, ast.Assignment{
			Target: target,
			Value:  value,
		})
	}

	return assignments, nil
}

// parseCallArgs parses the args mapping for a call step.
func parseCallArgs(node *yaml.Node, loc string) (map[string]interface{}, error) {
	if node.Kind != yaml.MappingNode {
		return nil, &ParseError{
			Message:  "call args must be a mapping",
			Location: loc,
		}
	}

	args := make(map[string]interface{})
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		args[key] = nodeToInterface(node.Content[i+1])
	}
	return args, nil
}

// parseSwitchConditions parses the conditions of a switch step.
func parseSwitchConditions(node *yaml.Node, loc string) ([]ast.SwitchCondition, error) {
	if node.Kind != yaml.SequenceNode {
		return nil, &ParseError{
			Message:  "switch must be a sequence",
			Location: loc,
		}
	}

	var conditions []ast.SwitchCondition
	for _, item := range node.Content {
		if item.Kind != yaml.MappingNode {
			return nil, &ParseError{
				Message:  "each switch condition must be a mapping",
				Location: loc,
			}
		}

		cond := ast.SwitchCondition{}
		for j := 0; j+1 < len(item.Content); j += 2 {
			key := item.Content[j].Value
			val := item.Content[j+1]

			switch key {
			case "condition":
				cond.Condition = nodeToInterface(val)
			case "next":
				cond.Next = val.Value
			case "steps":
				steps, err := parseSteps(val, loc+" (switch branch)")
				if err != nil {
					return nil, err
				}
				cond.Steps = steps
			case "assign":
				assignments, err := parseAssignments(val, loc+" (switch branch)")
				if err != nil {
					return nil, err
				}
				cond.Assign = assignments
			case "return":
				cond.Return = nodeToInterface(val)
				cond.HasReturn = true
			case "raise":
				cond.Raise = nodeToInterface(val)
			}
		}
		conditions = append(conditions, cond)
	}

	return conditions, nil
}

// parseFor parses a for-loop definition.
func parseFor(node *yaml.Node, loc string) (*ast.ForExpr, error) {
	if node.Kind != yaml.MappingNode {
		return nil, &ParseError{
			Message:  "for must be a mapping",
			Location: loc,
		}
	}

	f := &ast.ForExpr{}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		val := node.Content[i+1]

		switch key {
		case "value":
			f.Value = val.Value
		case "index":
			f.Index = val.Value
		case "in":
			f.In = nodeToInterface(val)
		case "range":
			if val.Kind != yaml.SequenceNode || len(val.Content) != 2 {
				return nil, &ParseError{
					Message:  "for range must be a two-element sequence [start, end]",
					Location: loc,
				}
			}
			f.Range = [2]interface{}{
				nodeToInterface(val.Content[0]),
				nodeToInterface(val.Content[1]),
			}
			f.HasRange = true
		case "steps":
			steps, err := parseSteps(val, loc+" (for body)")
			if err != nil {
				return nil, err
			}
			f.Steps = steps
		}
	}

	if f.Value == "" {
		return nil, &ParseError{
			Message:  "for loop must specify 'value'",
			Location: loc,
		}
	}

	return f, nil
}

// parseParallel parses a parallel step definition.
func parseParallel(node *yaml.Node, loc string) (*ast.ParallelExpr, error) {
	if node.Kind != yaml.MappingNode {
		return nil, &ParseError{
			Message:  "parallel must be a mapping",
			Location: loc,
		}
	}

	p := &ast.ParallelExpr{ExceptionPolicy: "unhandled"}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		val := node.Content[i+1]

		switch key {
		case "shared":
			if val.Kind != yaml.SequenceNode {
				return nil, &ParseError{
					Message:  "parallel shared must be a sequence",
					Location: loc,
				}
			}
			for _, item := range val.Content {
				p.Shared = append(p.Shared, item.Value)
			}
		case "branches":
			branches, err := parseParallelBranches(val, loc)
			if err != nil {
				return nil, err
			}
			p.Branches = branches
		case "for":
			forExpr, err := parseFor(val, loc+" (parallel for)")
			if err != nil {
				return nil, err
			}
			p.For = forExpr
		case "concurrency_limit":
			p.ConcurrencyLimit = intFromNode(val)
		case "exception_policy":
			p.ExceptionPolicy = val.Value
		}
	}

	return p, nil
}

// parseParallelBranches parses the branches of a parallel step.
func parseParallelBranches(node *yaml.Node, loc string) ([]*ast.ParallelBranch, error) {
	if node.Kind != yaml.SequenceNode {
		return nil, &ParseError{
			Message:  "parallel branches must be a sequence",
			Location: loc,
		}
	}

	var branches []*ast.ParallelBranch
	for _, item := range node.Content {
		if item.Kind != yaml.MappingNode || len(item.Content) != 2 {
			return nil, &ParseError{
				Message:  "each parallel branch must be a single-key mapping",
				Location: loc,
			}
		}

		branchName := item.Content[0].Value
		branchBody := item.Content[1]

		branch := &ast.ParallelBranch{Name: branchName}

		if branchBody.Kind == yaml.MappingNode {
			for j := 0; j+1 < len(branchBody.Content); j += 2 {
				key := branchBody.Content[j].Value
				val := branchBody.Content[j+1]
				if key == "steps" {
					steps, err := parseSteps(val, loc+fmt.Sprintf(" (branch '%s')", branchName))
					if err != nil {
						return nil, err
					}
					branch.Steps = steps
				}
			}
		}

		branches = append(branches, branch)
	}

	return branches, nil
}

// parseTrySteps parses the steps in a try block.
func parseTrySteps(node *yaml.Node, loc string) ([]*ast.Step, error) {
	// try block can be either a steps mapping or a direct sequence
	if node.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i].Value == "steps" {
				return parseSteps(node.Content[i+1], loc+" (try)")
			}
		}
		// No 'steps' key — treat as an inline step body (e.g. call/args/result directly).
		// GCP Cloud Workflows allows try blocks to contain a single step without a steps wrapper.
		inlineStep, err := parseStep("try_inline", node, loc+" (try)")
		if err != nil {
			return nil, err
		}
		return []*ast.Step{inlineStep}, nil
	}
	if node.Kind == yaml.SequenceNode {
		return parseSteps(node, loc+" (try)")
	}
	return nil, &ParseError{
		Message:  "try must be a mapping or sequence",
		Location: loc,
	}
}

// parseExcept parses the except clause of a try step.
func parseExcept(node *yaml.Node, loc string) (*ast.ExceptExpr, error) {
	if node.Kind != yaml.MappingNode {
		return nil, &ParseError{
			Message:  "except must be a mapping",
			Location: loc,
		}
	}

	except := &ast.ExceptExpr{}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		val := node.Content[i+1]

		switch key {
		case "as":
			except.As = val.Value
		case "steps":
			steps, err := parseSteps(val, loc+" (except)")
			if err != nil {
				return nil, err
			}
			except.Steps = steps
		}
	}

	return except, nil
}

// parseRetry parses the retry clause of a try step.
func parseRetry(node *yaml.Node, loc string) (*ast.RetryExpr, error) {
	if node.Kind != yaml.MappingNode {
		return nil, &ParseError{
			Message:  "retry must be a mapping",
			Location: loc,
		}
	}

	retry := &ast.RetryExpr{}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		val := node.Content[i+1]

		switch key {
		case "predicate":
			retry.Predicate = nodeToInterface(val)
		case "max_retries":
			retry.MaxRetries = intFromNode(val)
		case "backoff":
			backoff, err := parseBackoff(val, loc)
			if err != nil {
				return nil, err
			}
			retry.Backoff = backoff
		}
	}

	return retry, nil
}

// parseBackoff parses exponential backoff configuration.
func parseBackoff(node *yaml.Node, loc string) (*ast.BackoffExpr, error) {
	if node.Kind != yaml.MappingNode {
		return nil, &ParseError{
			Message:  "backoff must be a mapping",
			Location: loc,
		}
	}

	b := &ast.BackoffExpr{
		InitialDelay: 1,
		MaxDelay:     60,
		Multiplier:   2,
	}

	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		val := node.Content[i+1]

		switch key {
		case "initial_delay":
			b.InitialDelay = floatFromNode(val)
		case "max_delay":
			b.MaxDelay = floatFromNode(val)
		case "multiplier":
			b.Multiplier = floatFromNode(val)
		}
	}

	return b, nil
}

// nodeToInterface converts a yaml.Node to a Go interface{}.
// Strings starting with "${" are kept as-is for expression evaluation.
func nodeToInterface(node *yaml.Node) interface{} {
	switch node.Kind {
	case yaml.ScalarNode:
		return scalarToInterface(node)
	case yaml.SequenceNode:
		result := make([]interface{}, len(node.Content))
		for i, item := range node.Content {
			result[i] = nodeToInterface(item)
		}
		return result
	case yaml.MappingNode:
		result := make(map[string]interface{})
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i].Value
			result[key] = nodeToInterface(node.Content[i+1])
		}
		return result
	case yaml.AliasNode:
		return nodeToInterface(node.Alias)
	}
	return nil
}

// scalarToInterface converts a YAML scalar node to the appropriate Go type.
func scalarToInterface(node *yaml.Node) interface{} {
	if node.Tag == "!!null" || node.Value == "null" || node.Value == "~" || node.Value == "" {
		// Check if it's explicitly null vs empty string
		if node.Tag == "!!str" {
			return node.Value
		}
		if node.Value == "" && node.Tag == "" {
			return nil
		}
		return nil
	}

	switch node.Tag {
	case "!!bool":
		return node.Value == "true" || node.Value == "True" || node.Value == "TRUE" ||
			node.Value == "yes" || node.Value == "Yes" || node.Value == "YES"
	case "!!int":
		var i int64
		fmt.Sscanf(node.Value, "%d", &i)
		return i
	case "!!float":
		var f float64
		fmt.Sscanf(node.Value, "%f", &f)
		return f
	case "!!str":
		return node.Value
	}

	// Auto-detect type for untagged scalars
	val := node.Value

	// Check for boolean
	lower := strings.ToLower(val)
	if lower == "true" || lower == "yes" {
		return true
	}
	if lower == "false" || lower == "no" {
		return false
	}

	// Check for integer
	var i int64
	if n, _ := fmt.Sscanf(val, "%d", &i); n == 1 && fmt.Sprintf("%d", i) == val {
		return i
	}

	// Check for float
	var f float64
	if n, _ := fmt.Sscanf(val, "%g", &f); n == 1 {
		// Only treat as float if it contains a dot or scientific notation
		if strings.Contains(val, ".") || strings.ContainsAny(val, "eE") {
			return f
		}
	}

	// Default to string
	return val
}

// intFromNode extracts an integer from a scalar node.
func intFromNode(node *yaml.Node) int {
	var i int
	fmt.Sscanf(node.Value, "%d", &i)
	return i
}

// floatFromNode extracts a float from a scalar node.
func floatFromNode(node *yaml.Node) float64 {
	var f float64
	fmt.Sscanf(node.Value, "%g", &f)
	return f
}
