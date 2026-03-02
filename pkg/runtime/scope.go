// Package runtime implements the GCW workflow execution engine.
package runtime

import (
	"fmt"
	"strings"
	"sync"

	"github.com/lemonberrylabs/gcw-emulator/pkg/expr"
	"github.com/lemonberrylabs/gcw-emulator/pkg/types"
)

// VariableScope manages variable storage with parent scope chaining.
// Variables are looked up starting from the current scope and walking up
// the parent chain. New variables are always created in the current scope.
type VariableScope struct {
	parent    *VariableScope
	vars      map[string]types.Value
	mu        sync.RWMutex
	sharedMu  *sync.Mutex // shared mutex for parallel execution atomicity
}

// NewScope creates a new root scope.
func NewScope() *VariableScope {
	return &VariableScope{
		vars: make(map[string]types.Value),
	}
}

// NewChildScope creates a child scope that inherits from this scope.
func (s *VariableScope) NewChildScope() *VariableScope {
	return &VariableScope{
		parent:   s,
		vars:     make(map[string]types.Value),
		sharedMu: s.sharedMu,
	}
}

// NewSharedChildScope creates a child scope for parallel execution.
// It creates or reuses a shared mutex that ensures atomic read-modify-write
// operations on parent scope variables.
func (s *VariableScope) NewSharedChildScope() *VariableScope {
	sharedMu := s.sharedMu
	if sharedMu == nil {
		sharedMu = &sync.Mutex{}
	}
	return &VariableScope{
		parent:   s,
		vars:     make(map[string]types.Value),
		sharedMu: sharedMu,
	}
}

// LockShared acquires the shared mutex if it exists.
func (s *VariableScope) LockShared() {
	if s.sharedMu != nil {
		s.sharedMu.Lock()
	}
}

// UnlockShared releases the shared mutex if it exists.
func (s *VariableScope) UnlockShared() {
	if s.sharedMu != nil {
		s.sharedMu.Unlock()
	}
}

// Get retrieves a variable value, searching up the scope chain.
func (s *VariableScope) Get(name string) (types.Value, error) {
	s.mu.RLock()
	v, ok := s.vars[name]
	s.mu.RUnlock()
	if ok {
		return v, nil
	}
	if s.parent != nil {
		return s.parent.Get(name)
	}
	return types.Null, types.NewKeyError(fmt.Sprintf("variable '%s' not found", name))
}

// Set sets a variable in the scope where it exists, or creates it in this scope.
func (s *VariableScope) Set(name string, value types.Value) {
	// Check if variable exists in parent scopes
	if s.existsInParent(name) {
		s.setInParent(name, value)
		return
	}
	s.mu.Lock()
	s.vars[name] = value
	s.mu.Unlock()
}

// SetLocal sets a variable in this scope only (no parent search).
func (s *VariableScope) SetLocal(name string, value types.Value) {
	s.mu.Lock()
	s.vars[name] = value
	s.mu.Unlock()
}

// existsInParent checks if a variable exists in any parent scope.
func (s *VariableScope) existsInParent(name string) bool {
	if s.parent == nil {
		return false
	}
	s.parent.mu.RLock()
	_, ok := s.parent.vars[name]
	s.parent.mu.RUnlock()
	if ok {
		return true
	}
	return s.parent.existsInParent(name)
}

// setInParent sets a variable in the parent scope where it's found.
func (s *VariableScope) setInParent(name string, value types.Value) {
	if s.parent == nil {
		return
	}
	s.parent.mu.RLock()
	_, ok := s.parent.vars[name]
	s.parent.mu.RUnlock()
	if ok {
		s.parent.mu.Lock()
		s.parent.vars[name] = value
		s.parent.mu.Unlock()
		return
	}
	s.parent.setInParent(name, value)
}

// Exists checks if a variable exists in this scope or any parent.
func (s *VariableScope) Exists(name string) bool {
	s.mu.RLock()
	_, ok := s.vars[name]
	s.mu.RUnlock()
	if ok {
		return true
	}
	if s.parent != nil {
		return s.parent.Exists(name)
	}
	return false
}

// ScopeAdapter adapts a VariableScope to implement the expr.Scope interface.
type ScopeAdapter struct {
	scope   *VariableScope
	funcMap FunctionRegistry
}

// FunctionRegistry provides function lookup for expression evaluation.
type FunctionRegistry interface {
	// CallFunction calls a named function with the given arguments.
	CallFunction(name string, args []types.Value) (types.Value, error)
}

// NewScopeAdapter creates a scope adapter for expression evaluation.
func NewScopeAdapter(scope *VariableScope, funcs FunctionRegistry) *ScopeAdapter {
	return &ScopeAdapter{scope: scope, funcMap: funcs}
}

// GetVariable implements expr.Scope.
func (a *ScopeAdapter) GetVariable(name string) (types.Value, error) {
	return a.scope.Get(name)
}

// CallFunction implements expr.Scope.
func (a *ScopeAdapter) CallFunction(name string, args []types.Value) (types.Value, error) {
	if a.funcMap != nil {
		return a.funcMap.CallFunction(name, args)
	}
	return types.Null, fmt.Errorf("function '%s' not found", name)
}

// EvalValue evaluates a parsed YAML value (which may contain ${} expressions)
// within the given scope.
func EvalValue(v interface{}, scope *VariableScope, funcs FunctionRegistry) (types.Value, error) {
	node, err := expr.ParseValue(v)
	if err != nil {
		return types.Null, err
	}
	adapter := NewScopeAdapter(scope, funcs)
	return expr.Evaluate(node, adapter)
}

// SetByPath sets a value by dotted/index path (e.g., "obj.key", "list[0]").
func SetByPath(scope *VariableScope, path string, value types.Value) error {
	parts := parseAssignmentPath(path)
	if len(parts) == 0 {
		return fmt.Errorf("empty assignment path")
	}

	if len(parts) == 1 {
		scope.Set(parts[0].name, value)
		return nil
	}

	// Get the root variable
	rootName := parts[0].name
	root, err := scope.Get(rootName)
	if err != nil {
		return err
	}

	// Navigate to the parent of the target and set the value
	current := root
	for i := 1; i < len(parts)-1; i++ {
		current, err = accessPart(current, parts[i])
		if err != nil {
			return err
		}
	}

	// Set the final value
	last := parts[len(parts)-1]
	err = setPart(current, last, value)
	if err != nil {
		return err
	}

	// Write back the root (maps and lists are reference types, so this should propagate)
	scope.Set(rootName, root)
	return nil
}

type pathPart struct {
	name    string // property name
	index   int    // array index (only used if isIndex is true)
	isIndex bool
}

func parseAssignmentPath(path string) []pathPart {
	var parts []pathPart
	i := 0

	for i < len(path) {
		if path[i] == '[' {
			// Index or key access
			j := strings.Index(path[i:], "]")
			if j == -1 {
				break
			}
			indexStr := path[i+1 : i+j]
			// Check if it's a string key (quoted)
			if len(indexStr) >= 2 && (indexStr[0] == '"' && indexStr[len(indexStr)-1] == '"') {
				// String key access like data["phone"]
				keyName := indexStr[1 : len(indexStr)-1]
				parts = append(parts, pathPart{name: keyName})
			} else {
				var idx int
				fmt.Sscanf(indexStr, "%d", &idx)
				parts = append(parts, pathPart{index: idx, isIndex: true})
			}
			i += j + 1
			if i < len(path) && path[i] == '.' {
				i++ // skip separator
			}
		} else {
			// Property access
			j := i
			for j < len(path) && path[j] != '.' && path[j] != '[' {
				j++
			}
			parts = append(parts, pathPart{name: path[i:j]})
			i = j
			if i < len(path) && path[i] == '.' {
				i++ // skip separator
			}
		}
	}

	return parts
}

func accessPart(v types.Value, p pathPart) (types.Value, error) {
	if p.isIndex {
		if v.Type() != types.TypeList {
			return types.Null, types.NewTypeError("index access on non-list")
		}
		list := v.AsList()
		if p.index < 0 || p.index >= len(list) {
			return types.Null, types.NewIndexError(
				fmt.Sprintf("index %d out of range (length %d)", p.index, len(list)))
		}
		return list[p.index], nil
	}

	if v.Type() != types.TypeMap {
		return types.Null, types.NewTypeError(
			fmt.Sprintf("property access '%s' on non-map (%s)", p.name, v.Type()))
	}
	val, ok := v.AsMap().Get(p.name)
	if !ok {
		return types.Null, types.NewKeyError(
			fmt.Sprintf("key '%s' not found", p.name))
	}
	return val, nil
}

func setPart(v types.Value, p pathPart, value types.Value) error {
	if p.isIndex {
		if v.Type() != types.TypeList {
			return types.NewTypeError("index assignment on non-list")
		}
		list := v.AsList()
		if p.index < 0 || p.index >= len(list) {
			return types.NewIndexError(
				fmt.Sprintf("index %d out of range (length %d)", p.index, len(list)))
		}
		list[p.index] = value
		return nil
	}

	if v.Type() != types.TypeMap {
		return types.NewTypeError(
			fmt.Sprintf("property assignment '%s' on non-map (%s)", p.name, v.Type()))
	}
	v.AsMap().Set(p.name, value)
	return nil
}
