package runtime

import (
	"context"
	"testing"

	"github.com/lemonberrylabs/gcw-emulator/pkg/parser"
	"github.com/lemonberrylabs/gcw-emulator/pkg/stdlib"
	"github.com/lemonberrylabs/gcw-emulator/pkg/types"
)

func runWorkflow(t *testing.T, source string, args types.Value) types.Value {
	t.Helper()

	wf, err := parser.Parse([]byte(source))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	funcs := stdlib.NewRegistry()
	engine := NewEngine(wf, funcs)

	result, err := engine.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("execution error: %v", err)
	}
	return result
}

func runWorkflowExpectError(t *testing.T, source string, args types.Value) error {
	t.Helper()

	wf, err := parser.Parse([]byte(source))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	funcs := stdlib.NewRegistry()
	engine := NewEngine(wf, funcs)

	_, err = engine.Execute(context.Background(), args)
	if err == nil {
		t.Fatal("expected error but got nil")
	}
	return err
}

func TestAssignAndReturn(t *testing.T) {
	result := runWorkflow(t, `
main:
  steps:
    - init:
        assign:
          - x: 42
    - done:
        return: ${x}
`, types.Null)

	if !result.Equal(types.NewInt(42)) {
		t.Errorf("got %v, want 42", result)
	}
}

func TestAssignExpression(t *testing.T) {
	result := runWorkflow(t, `
main:
  steps:
    - init:
        assign:
          - a: 10
          - b: 20
          - c: ${a + b}
    - done:
        return: ${c}
`, types.Null)

	if !result.Equal(types.NewInt(30)) {
		t.Errorf("got %v, want 30", result)
	}
}

func TestReturnString(t *testing.T) {
	result := runWorkflow(t, `
main:
  steps:
    - done:
        return: "hello world"
`, types.Null)

	if !result.Equal(types.NewString("hello world")) {
		t.Errorf("got %v, want 'hello world'", result)
	}
}

func TestNextJump(t *testing.T) {
	result := runWorkflow(t, `
main:
  steps:
    - step1:
        assign:
          - x: 1
        next: step3
    - step2:
        assign:
          - x: 999
    - step3:
        return: ${x}
`, types.Null)

	if !result.Equal(types.NewInt(1)) {
		t.Errorf("got %v, want 1 (step2 should be skipped)", result)
	}
}

func TestNextEnd(t *testing.T) {
	result := runWorkflow(t, `
main:
  steps:
    - step1:
        assign:
          - x: 42
        next: end
    - step2:
        assign:
          - x: 999
    - done:
        return: ${x}
`, types.Null)

	// With next: end, execution ends before reaching return, so result is null
	if !result.IsNull() {
		t.Errorf("got %v, want null (next: end should terminate)", result)
	}
}

func TestSwitchBasic(t *testing.T) {
	result := runWorkflow(t, `
main:
  steps:
    - init:
        assign:
          - x: 5
    - check:
        switch:
          - condition: ${x < 10}
            return: "small"
          - condition: ${x >= 10}
            return: "big"
`, types.Null)

	if !result.Equal(types.NewString("small")) {
		t.Errorf("got %v, want 'small'", result)
	}
}

func TestSwitchWithNext(t *testing.T) {
	result := runWorkflow(t, `
main:
  steps:
    - init:
        assign:
          - x: 50
    - check:
        switch:
          - condition: ${x < 10}
            next: small_handler
          - condition: ${x >= 10}
            next: big_handler
    - small_handler:
        return: "small"
    - big_handler:
        return: "big"
`, types.Null)

	if !result.Equal(types.NewString("big")) {
		t.Errorf("got %v, want 'big'", result)
	}
}

func TestForLoop(t *testing.T) {
	result := runWorkflow(t, `
main:
  steps:
    - init:
        assign:
          - total: 0
    - loop:
        for:
          value: v
          in: [1, 2, 3, 4, 5]
          steps:
            - add:
                assign:
                  - total: ${total + v}
    - done:
        return: ${total}
`, types.Null)

	if !result.Equal(types.NewInt(15)) {
		t.Errorf("got %v, want 15", result)
	}
}

func TestForRange(t *testing.T) {
	result := runWorkflow(t, `
main:
  steps:
    - init:
        assign:
          - total: 0
    - loop:
        for:
          value: v
          range: [1, 5]
          steps:
            - add:
                assign:
                  - total: ${total + v}
    - done:
        return: ${total}
`, types.Null)

	if !result.Equal(types.NewInt(15)) {
		t.Errorf("got %v, want 15", result)
	}
}

func TestForBreak(t *testing.T) {
	result := runWorkflow(t, `
main:
  steps:
    - init:
        assign:
          - total: 0
    - loop:
        for:
          value: v
          in: [1, 2, 3, 4, 5]
          steps:
            - check:
                switch:
                  - condition: ${v == 4}
                    next: break
            - add:
                assign:
                  - total: ${total + v}
    - done:
        return: ${total}
`, types.Null)

	// 1 + 2 + 3 = 6 (breaks when v == 4)
	if !result.Equal(types.NewInt(6)) {
		t.Errorf("got %v, want 6", result)
	}
}

func TestForContinue(t *testing.T) {
	result := runWorkflow(t, `
main:
  steps:
    - init:
        assign:
          - total: 0
    - loop:
        for:
          value: v
          in: [1, 2, 3, 4, 5]
          steps:
            - check:
                switch:
                  - condition: ${v == 3}
                    next: continue
            - add:
                assign:
                  - total: ${total + v}
    - done:
        return: ${total}
`, types.Null)

	// 1 + 2 + 4 + 5 = 12 (skips v == 3)
	if !result.Equal(types.NewInt(12)) {
		t.Errorf("got %v, want 12", result)
	}
}

func TestForWithIndex(t *testing.T) {
	result := runWorkflow(t, `
main:
  steps:
    - init:
        assign:
          - sum_indices: 0
    - loop:
        for:
          value: v
          index: i
          in: ["a", "b", "c"]
          steps:
            - add:
                assign:
                  - sum_indices: ${sum_indices + i}
    - done:
        return: ${sum_indices}
`, types.Null)

	// 0 + 1 + 2 = 3
	if !result.Equal(types.NewInt(3)) {
		t.Errorf("got %v, want 3", result)
	}
}

func TestSubworkflowCall(t *testing.T) {
	result := runWorkflow(t, `
main:
  steps:
    - call_sub:
        call: greet
        args:
          name: "World"
        result: msg
    - done:
        return: ${msg}

greet:
  params: [name]
  steps:
    - build:
        return: ${"Hello " + name}
`, types.Null)

	if !result.Equal(types.NewString("Hello World")) {
		t.Errorf("got %v, want 'Hello World'", result)
	}
}

func TestSubworkflowDefaultParam(t *testing.T) {
	result := runWorkflow(t, `
main:
  steps:
    - call_sub:
        call: greet
        args:
          first_name: "Ada"
        result: msg
    - done:
        return: ${msg}

greet:
  params:
    - first_name
    - last_name: "Lovelace"
  steps:
    - build:
        return: ${first_name + " " + last_name}
`, types.Null)

	if !result.Equal(types.NewString("Ada Lovelace")) {
		t.Errorf("got %v, want 'Ada Lovelace'", result)
	}
}

func TestTryExcept(t *testing.T) {
	result := runWorkflow(t, `
main:
  steps:
    - risky:
        try:
          steps:
            - fail:
                raise:
                  message: "something broke"
                  code: 42
        except:
          as: e
          steps:
            - handle:
                return: ${e.message}
`, types.Null)

	if !result.Equal(types.NewString("something broke")) {
		t.Errorf("got %v, want 'something broke'", result)
	}
}

func TestRaise(t *testing.T) {
	err := runWorkflowExpectError(t, `
main:
  steps:
    - fail:
        raise:
          message: "custom error"
          code: 99
`, types.Null)

	we, ok := err.(*types.WorkflowError)
	if !ok {
		t.Fatalf("expected WorkflowError, got %T: %v", err, err)
	}
	if we.Message != "custom error" {
		t.Errorf("got message %q, want 'custom error'", we.Message)
	}
	if we.Code != 99 {
		t.Errorf("got code %d, want 99", we.Code)
	}
}

func TestMainWithArgs(t *testing.T) {
	args := types.NewOrderedMap()
	args.Set("name", types.NewString("Cloud"))

	result := runWorkflow(t, `
main:
  params: [args]
  steps:
    - done:
        return: ${args.name}
`, types.NewMap(args))

	if !result.Equal(types.NewString("Cloud")) {
		t.Errorf("got %v, want 'Cloud'", result)
	}
}

func TestStdlibLen(t *testing.T) {
	result := runWorkflow(t, `
main:
  steps:
    - init:
        assign:
          - items: [1, 2, 3]
          - n: ${len(items)}
    - done:
        return: ${n}
`, types.Null)

	if !result.Equal(types.NewInt(3)) {
		t.Errorf("got %v, want 3", result)
	}
}

func TestStdlibKeys(t *testing.T) {
	result := runWorkflow(t, `
main:
  steps:
    - init:
        assign:
          - m:
              a: 1
              b: 2
          - k: ${keys(m)}
          - n: ${len(k)}
    - done:
        return: ${n}
`, types.Null)

	if !result.Equal(types.NewInt(2)) {
		t.Errorf("got %v, want 2", result)
	}
}

func TestStdlibDefault(t *testing.T) {
	result := runWorkflow(t, `
main:
  steps:
    - init:
        assign:
          - val: ${default(null, "fallback")}
    - done:
        return: ${val}
`, types.Null)

	if !result.Equal(types.NewString("fallback")) {
		t.Errorf("got %v, want 'fallback'", result)
	}
}

func TestStdlibType(t *testing.T) {
	result := runWorkflow(t, `
main:
  steps:
    - init:
        assign:
          - t: ${type(42)}
    - done:
        return: ${t}
`, types.Null)

	if !result.Equal(types.NewString("int")) {
		t.Errorf("got %v, want 'int'", result)
	}
}

func TestMapPropertyAccess(t *testing.T) {
	result := runWorkflow(t, `
main:
  steps:
    - init:
        assign:
          - person:
              name: "Alice"
              age: 30
    - done:
        return: ${person.name}
`, types.Null)

	if !result.Equal(types.NewString("Alice")) {
		t.Errorf("got %v, want 'Alice'", result)
	}
}

func TestListIndexAccess(t *testing.T) {
	result := runWorkflow(t, `
main:
  steps:
    - init:
        assign:
          - items: ["a", "b", "c"]
    - done:
        return: ${items[1]}
`, types.Null)

	if !result.Equal(types.NewString("b")) {
		t.Errorf("got %v, want 'b'", result)
	}
}

func TestNestedMapAccess(t *testing.T) {
	result := runWorkflow(t, `
main:
  steps:
    - init:
        assign:
          - data:
              user:
                name: "Bob"
    - done:
        return: ${data.user.name}
`, types.Null)

	if !result.Equal(types.NewString("Bob")) {
		t.Errorf("got %v, want 'Bob'", result)
	}
}

func TestMapMutation(t *testing.T) {
	result := runWorkflow(t, `
main:
  steps:
    - init:
        assign:
          - m:
              a: 1
          - m.b: 2
    - done:
        return: ${m.b}
`, types.Null)

	if !result.Equal(types.NewInt(2)) {
		t.Errorf("got %v, want 2", result)
	}
}

func TestRecursionDepthLimit(t *testing.T) {
	err := runWorkflowExpectError(t, `
main:
  steps:
    - recurse:
        call: infinite
        result: r

infinite:
  steps:
    - again:
        call: infinite
        result: r
    - done:
        return: ${r}
`, types.Null)

	we, ok := err.(*types.WorkflowError)
	if !ok {
		t.Fatalf("expected WorkflowError, got %T: %v", err, err)
	}
	if !we.HasTag(types.TagRecursionError) {
		t.Errorf("expected RecursionError tag, got %v", we.Tags)
	}
}

func TestNestedSteps(t *testing.T) {
	result := runWorkflow(t, `
main:
  steps:
    - outer:
        steps:
          - inner1:
              assign:
                - x: 10
          - inner2:
              assign:
                - y: 20
    - done:
        return: ${x + y}
`, types.Null)

	if !result.Equal(types.NewInt(30)) {
		t.Errorf("got %v, want 30", result)
	}
}
