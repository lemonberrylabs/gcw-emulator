package stdlib

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lemonberrylabs/gcw-emulator/pkg/types"
)

func TestCallbackStore_IDsAreUnguessable(t *testing.T) {
	s := GetCallbackStore()
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := s.Create()
		defer s.Delete(id)
		if !strings.HasPrefix(id, "callback-") || len(id) < len("callback-")+32 {
			t.Fatalf("expected callback-<32 hex chars> id, got %q", id)
		}
		if seen[id] {
			t.Fatalf("duplicate callback id %q", id)
		}
		seen[id] = true
	}
}

func TestCallbackStore_DeliverBeforeAwaitIsBuffered(t *testing.T) {
	s := GetCallbackStore()
	id := s.Create()

	if err := s.Deliver(id, types.NewString("early")); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	val, err := s.Await(context.Background(), id, time.Second)
	if err != nil {
		t.Fatalf("Await: %v", err)
	}
	if val.AsString() != "early" {
		t.Fatalf("expected buffered value, got %v", val)
	}
}

func TestCallbackStore_OnlyOneDeliverySucceeds(t *testing.T) {
	s := GetCallbackStore()
	id := s.Create()

	// Start the awaiter first so consumption races with the deliveries.
	type awaitResult struct {
		val types.Value
		err error
	}
	awaitCh := make(chan awaitResult, 1)
	go func() {
		v, err := s.Await(context.Background(), id, 5*time.Second)
		awaitCh <- awaitResult{v, err}
	}()

	const attempts = 8
	var wg sync.WaitGroup
	errs := make([]error, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			errs[n] = s.Deliver(id, types.NewInt(int64(n)))
		}(i)
	}
	wg.Wait()

	succeeded := 0
	for _, err := range errs {
		if err == nil {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Fatalf("expected exactly one successful delivery, got %d", succeeded)
	}

	res := <-awaitCh
	if res.err != nil {
		t.Fatalf("Await: %v", res.err)
	}

	// The callback is consumed; any further delivery must fail.
	if err := s.Deliver(id, types.NewString("late")); err == nil {
		t.Fatal("expected error delivering to a consumed callback")
	}
}
