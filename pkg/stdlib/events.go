package stdlib

import (
	"fmt"
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

// Await waits for a callback to be triggered or times out.
func (s *CallbackStore) Await(id string, timeout time.Duration) (types.Value, error) {
	s.mu.Lock()
	ch, ok := s.callbacks[id]
	s.mu.Unlock()

	if !ok {
		return types.Null, types.NewValueError(fmt.Sprintf("callback '%s' not found", id))
	}

	select {
	case val := <-ch:
		return val, nil
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

	ch <- data
	return nil
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

// registerEvents registers events.* functions.
func (r *Registry) registerEvents() {
	r.Register("events.create_callback_endpoint", eventsCreateCallback)
	r.Register("events.await_callback", eventsAwaitCallback)
}

func eventsCreateCallback(args []types.Value) (types.Value, error) {
	id := globalCallbackStore.Create()

	// Return callback info as a map
	m := types.NewOrderedMap()
	m.Set("callback_id", types.NewString(id))
	return types.NewMap(m), nil
}

func eventsAwaitCallback(args []types.Value) (types.Value, error) {
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
	return globalCallbackStore.Await(callbackID, timeout)
}
