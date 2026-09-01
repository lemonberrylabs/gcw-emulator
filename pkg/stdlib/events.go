package stdlib

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/lemonberrylabs/gcw-emulator/pkg/types"
)

// CallbackStore manages pending callbacks for the emulator.
type CallbackStore struct {
	mu        sync.Mutex
	callbacks map[string]chan types.Value // callbackID -> channel
	counter   int64
}

// globalCallbackStore is the singleton callback store.
var globalCallbackStore = &CallbackStore{
	callbacks: make(map[string]chan types.Value),
}

// GetCallbackStore returns the global callback store.
func GetCallbackStore() *CallbackStore {
	return globalCallbackStore
}

// Create creates a new callback and returns its ID.
func (s *CallbackStore) Create() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counter++
	id := fmt.Sprintf("callback-%d", s.counter)
	s.callbacks[id] = make(chan types.Value, 1)
	return id
}

// Await waits for a callback to be triggered, times out, or is cancelled via context.
func (s *CallbackStore) Await(ctx context.Context, id string, timeout time.Duration) (types.Value, error) {
	s.mu.Lock()
	ch, ok := s.callbacks[id]
	s.mu.Unlock()

	if !ok {
		return types.Null, types.NewValueError(fmt.Sprintf("callback '%s' not found", id))
	}

	select {
	case val := <-ch:
		// Remove the consumed callback so later deliveries report 404 instead
		// of silently filling the channel buffer with nobody waiting.
		s.mu.Lock()
		delete(s.callbacks, id)
		s.mu.Unlock()
		return val, nil
	case <-ctx.Done():
		s.mu.Lock()
		delete(s.callbacks, id)
		s.mu.Unlock()
		return types.Null, ctx.Err()
	case <-time.After(timeout):
		s.mu.Lock()
		delete(s.callbacks, id)
		s.mu.Unlock()
		return types.Null, types.NewTimeoutError("callback timed out")
	}
}

// Deliver sends data to a pending callback.
func (s *CallbackStore) Deliver(id string, data types.Value) error {
	s.mu.Lock()
	ch, ok := s.callbacks[id]
	s.mu.Unlock()

	if !ok {
		return fmt.Errorf("callback '%s' not found or already completed", id)
	}

	select {
	case ch <- data:
		return nil
	default:
		return fmt.Errorf("callback '%s' already delivered", id)
	}
}

// Delete removes a pending callback, e.g. when its owning execution ends.
func (s *CallbackStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.callbacks, id)
}

// List returns all pending callback IDs.
func (s *CallbackStore) List() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.callbacks))
	for id := range s.callbacks {
		ids = append(ids, id)
	}
	return ids
}

// CallbackRegistrar is implemented by the API server so that callback
// endpoints created during an execution can be listed and delivered through
// the REST API. It returns the absolute URL of the callback endpoint.
type CallbackRegistrar interface {
	RegisterCallback(executionName, callbackID, method string) (url string)
}

type callbackContextKey string

const (
	callbackRegistrarKey callbackContextKey = "callbackRegistrar"
	executionNameKey     callbackContextKey = "executionName"
)

// WithCallbackRegistrar returns a context that carries the registrar and the
// owning execution name, used by events.create_callback_endpoint.
func WithCallbackRegistrar(ctx context.Context, registrar CallbackRegistrar, executionName string) context.Context {
	ctx = context.WithValue(ctx, callbackRegistrarKey, registrar)
	return context.WithValue(ctx, executionNameKey, executionName)
}

// registerEvents registers events.* functions.
func (r *Registry) registerEvents() {
	r.Register("events.create_callback_endpoint", eventsCreateCallback)
	r.Register("events.await_callback", eventsAwaitCallback)
}

// supportedCallbackMethods is the set of HTTP methods real GCW accepts for
// http_callback_method.
var supportedCallbackMethods = map[string]bool{
	"GET": true, "HEAD": true, "POST": true, "PUT": true,
	"DELETE": true, "OPTIONS": true, "PATCH": true,
}

func eventsCreateCallback(ctx context.Context, args []types.Value) (types.Value, error) {
	method := "POST"
	if len(args) > 0 && args[0].Type() == types.TypeMap {
		if m, ok := args[0].AsMap().Get("http_callback_method"); ok && m.Type() == types.TypeString {
			method = strings.ToUpper(m.AsString())
			if !supportedCallbackMethods[method] {
				return types.Null, types.NewValueError(fmt.Sprintf(
					"events.create_callback_endpoint: unsupported http_callback_method '%s'", m.AsString()))
			}
		}
	}

	id := globalCallbackStore.Create()

	// Register the endpoint with the API server (when running under one) so
	// it is listed by the executions callbacks API and reachable over HTTP.
	url := "/callbacks/" + id
	if registrar, ok := ctx.Value(callbackRegistrarKey).(CallbackRegistrar); ok {
		executionName, _ := ctx.Value(executionNameKey).(string)
		url = registrar.RegisterCallback(executionName, id, method)
	}

	// Return callback info as a map. Real GCW returns the endpoint URL; the
	// callback_id is kept for convenience.
	m := types.NewOrderedMap()
	m.Set("callback_id", types.NewString(id))
	m.Set("method", types.NewString(method))
	m.Set("url", types.NewString(url))
	return types.NewMap(m), nil
}

func eventsAwaitCallback(ctx context.Context, args []types.Value) (types.Value, error) {
	var callbackVal types.Value
	var timeoutSec float64 = 300 // default 5 minutes

	if len(args) > 0 && args[0].Type() == types.TypeMap {
		m := args[0].AsMap()
		if cb, ok := m.Get("callback"); ok {
			callbackVal = cb
		}
		if t, ok := m.Get("timeout"); ok {
			switch t.Type() {
			case types.TypeInt:
				timeoutSec = float64(t.AsInt())
			case types.TypeDouble:
				timeoutSec = t.AsDouble()
			}
		}
	}

	if callbackVal.IsNull() {
		return types.Null, types.NewValueError("events.await_callback: missing callback argument")
	}

	// Extract callback_id from the callback value
	var callbackID string
	if callbackVal.Type() == types.TypeMap {
		if id, ok := callbackVal.AsMap().Get("callback_id"); ok {
			callbackID = id.AsString()
		}
	}
	if callbackID == "" {
		return types.Null, types.NewValueError("events.await_callback: invalid callback value")
	}

	timeout := time.Duration(timeoutSec * float64(time.Second))
	return globalCallbackStore.Await(ctx, callbackID, timeout)
}
