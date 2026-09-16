// Package stdlib implements the GCW standard library functions.
package stdlib

import (
	"context"
	"fmt"

	"github.com/lemonberrylabs/gcw-emulator/pkg/types"
)

// StdlibFunc is a standard library function signature.
type StdlibFunc func(ctx context.Context, args []types.Value) (types.Value, error)

// Registry holds all standard library functions and serves as a FunctionRegistry.
type Registry struct {
	funcs map[string]StdlibFunc
}

// NewRegistry creates a new stdlib registry with all built-in functions registered.
func NewRegistry() *Registry {
	r := &Registry{
		funcs: make(map[string]StdlibFunc),
	}
	r.registerExpressionHelpers()
	r.registerSys()
	r.registerJSON()
	r.registerBase64()
	r.registerMath()
	r.registerText()
	r.registerList()
	r.registerMapFuncs()
	r.registerUUID()
	r.registerTime()
	r.registerHash()
	r.registerEvents()
	return r
}

// CallFunction implements FunctionRegistry.
func (r *Registry) CallFunction(ctx context.Context, name string, args []types.Value) (types.Value, error) {
	fn, ok := r.funcs[name]
	if !ok {
		return types.Null, fmt.Errorf("unknown function '%s'", name)
	}
	return fn(ctx, args)
}

// Register adds a function to the registry.
func (r *Registry) Register(name string, fn StdlibFunc) {
	r.funcs[name] = fn
}

// Has reports whether a function with the given name is registered.
func (r *Registry) Has(name string) bool {
	_, ok := r.funcs[name]
	return ok
}

// requireArgs checks that the number of args is in range.
func requireArgs(name string, args []types.Value, min, max int) error {
	if len(args) < min || len(args) > max {
		if min == max {
			return fmt.Errorf("%s expects %d argument(s), got %d", name, min, len(args))
		}
		return fmt.Errorf("%s expects %d-%d arguments, got %d", name, min, max, len(args))
	}
	return nil
}
