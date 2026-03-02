package expr

import (
	"fmt"
	"testing"

	"github.com/lemonberrylabs/gcw-emulator/pkg/types"
)

// testScope implements Scope for testing.
type testScope struct {
	vars  map[string]types.Value
	funcs map[string]func([]types.Value) (types.Value, error)
}

func newTestScope() *testScope {
	return &testScope{
		vars:  make(map[string]types.Value),
		funcs: make(map[string]func([]types.Value) (types.Value, error)),
	}
}

func (s *testScope) GetVariable(name string) (types.Value, error) {
	v, ok := s.vars[name]
	if !ok {
		return types.Null, types.NewKeyError(fmt.Sprintf("variable '%s' not found", name))
	}
	return v, nil
}

func (s *testScope) CallFunction(name string, args []types.Value) (types.Value, error) {
	fn, ok := s.funcs[name]
	if !ok {
		return types.Null, fmt.Errorf("function '%s' not found", name)
	}
	return fn(args)
}

func TestLiteralExpressions(t *testing.T) {
	scope := newTestScope()

	tests := []struct {
		input string
		want  types.Value
	}{
		{"42", types.NewInt(42)},
		{"0", types.NewInt(0)},
		{"3.14", types.NewDouble(3.14)},
		{`"hello"`, types.NewString("hello")},
		{`"hello world"`, types.NewString("hello world")},
		{`""`, types.NewString("")},
		{"true", types.NewBool(true)},
		{"false", types.NewBool(false)},
		{"null", types.Null},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			node, err := ParseExpression(tt.input)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			got, err := Evaluate(node, scope)
			if err != nil {
				t.Fatalf("eval error: %v", err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestArithmeticExpressions(t *testing.T) {
	scope := newTestScope()

	tests := []struct {
		input string
		want  types.Value
	}{
		{"1 + 2", types.NewInt(3)},
		{"10 - 3", types.NewInt(7)},
		{"4 * 5", types.NewInt(20)},
		{"10 / 3", types.NewDouble(10.0 / 3.0)},
		{"10 % 3", types.NewInt(1)},
		{"10 // 3", types.NewInt(3)},
		{"2 + 3 * 4", types.NewInt(14)},     // precedence
		{"(2 + 3) * 4", types.NewInt(20)},    // parens
		{"-5", types.NewInt(-5)},             // unary minus
		{"1.5 + 2.5", types.NewDouble(4.0)},  // float math
		{"1 + 2.0", types.NewDouble(3.0)},     // int + float = float
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			node, err := ParseExpression(tt.input)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			got, err := Evaluate(node, scope)
			if err != nil {
				t.Fatalf("eval error: %v", err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestComparisonExpressions(t *testing.T) {
	scope := newTestScope()

	tests := []struct {
		input string
		want  bool
	}{
		{"1 == 1", true},
		{"1 == 2", false},
		{"1 != 2", true},
		{"1 < 2", true},
		{"2 < 1", false},
		{"2 > 1", true},
		{"1 <= 1", true},
		{"1 >= 2", false},
		{`"abc" == "abc"`, true},
		{`"abc" < "abd"`, true},
		{`"abc" > "abb"`, true},
		{"null == null", true},
		{"null != 0", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			node, err := ParseExpression(tt.input)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			got, err := Evaluate(node, scope)
			if err != nil {
				t.Fatalf("eval error: %v", err)
			}
			if got.Type() != types.TypeBool || got.AsBool() != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLogicalExpressions(t *testing.T) {
	scope := newTestScope()

	tests := []struct {
		input string
		want  bool
	}{
		{"true and true", true},
		{"true and false", false},
		{"false or true", true},
		{"false or false", false},
		{"not true", false},
		{"not false", true},
		{"true and not false", true},
		{"(true or false) and true", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			node, err := ParseExpression(tt.input)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			got, err := Evaluate(node, scope)
			if err != nil {
				t.Fatalf("eval error: %v", err)
			}
			if !got.Truthy() != !tt.want {
				t.Errorf("got %v, want truthy=%v", got, tt.want)
			}
		})
	}
}

func TestStringConcatenation(t *testing.T) {
	scope := newTestScope()

	node, err := ParseExpression(`"hello" + " " + "world"`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	got, err := Evaluate(node, scope)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if got.Type() != types.TypeString || got.AsString() != "hello world" {
		t.Errorf("got %v, want 'hello world'", got)
	}
}

func TestStringPlusNonStringIsTypeError(t *testing.T) {
	scope := newTestScope()

	node, err := ParseExpression(`"count: " + 42`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Evaluate(node, scope)
	if err == nil {
		t.Fatal("expected TypeError for string + int")
	}
	we, ok := err.(*types.WorkflowError)
	if !ok {
		t.Fatalf("expected WorkflowError, got %T", err)
	}
	if !we.HasTag(types.TagTypeError) {
		t.Errorf("expected TypeError tag, got %v", we.Tags)
	}
}

func TestVariableAccess(t *testing.T) {
	scope := newTestScope()
	scope.vars["x"] = types.NewInt(42)
	scope.vars["name"] = types.NewString("Alice")

	m := types.NewOrderedMap()
	m.Set("key", types.NewString("value"))
	m.Set("count", types.NewInt(5))
	scope.vars["obj"] = types.NewMap(m)

	scope.vars["items"] = types.NewList([]types.Value{
		types.NewInt(10),
		types.NewInt(20),
		types.NewInt(30),
	})

	tests := []struct {
		input string
		want  types.Value
	}{
		{"x", types.NewInt(42)},
		{"name", types.NewString("Alice")},
		{"x + 1", types.NewInt(43)},
		{`obj.key`, types.NewString("value")},
		{`obj.count`, types.NewInt(5)},
		{`items[0]`, types.NewInt(10)},
		{`items[2]`, types.NewInt(30)},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			node, err := ParseExpression(tt.input)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			got, err := Evaluate(node, scope)
			if err != nil {
				t.Fatalf("eval error: %v", err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInExpression(t *testing.T) {
	scope := newTestScope()
	scope.vars["my_list"] = types.NewList([]types.Value{
		types.NewInt(1), types.NewInt(2), types.NewInt(3),
	})

	m := types.NewOrderedMap()
	m.Set("a", types.NewInt(1))
	scope.vars["my_map"] = types.NewMap(m)

	tests := []struct {
		input string
		want  bool
	}{
		{`2 in my_list`, true},
		{`5 in my_list`, false},
		{`5 not in my_list`, true},
		{`"a" in my_map`, true},
		{`"b" in my_map`, false},
		{`"b" not in my_map`, true},
		{`"lo" in "hello"`, true},
		{`"xyz" in "hello"`, false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			node, err := ParseExpression(tt.input)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			got, err := Evaluate(node, scope)
			if err != nil {
				t.Fatalf("eval error: %v", err)
			}
			if got.Type() != types.TypeBool || got.AsBool() != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFunctionCall(t *testing.T) {
	scope := newTestScope()
	scope.vars["my_list"] = types.NewList([]types.Value{
		types.NewInt(1), types.NewInt(2), types.NewInt(3),
	})
	scope.funcs["len"] = func(args []types.Value) (types.Value, error) {
		if len(args) != 1 {
			return types.Null, fmt.Errorf("len expects 1 argument")
		}
		switch args[0].Type() {
		case types.TypeList:
			return types.NewInt(int64(len(args[0].AsList()))), nil
		case types.TypeString:
			return types.NewInt(int64(len(args[0].AsString()))), nil
		default:
			return types.Null, fmt.Errorf("len not supported for %s", args[0].Type())
		}
	}

	node, err := ParseExpression("len(my_list)")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	got, err := Evaluate(node, scope)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if !got.Equal(types.NewInt(3)) {
		t.Errorf("got %v, want 3", got)
	}
}

func TestDivisionByZero(t *testing.T) {
	scope := newTestScope()

	ops := []string{"10 / 0", "10 % 0", "10 // 0"}
	for _, op := range ops {
		t.Run(op, func(t *testing.T) {
			node, err := ParseExpression(op)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			_, err = Evaluate(node, scope)
			if err == nil {
				t.Fatal("expected ZeroDivisionError")
			}
			we, ok := err.(*types.WorkflowError)
			if !ok {
				t.Fatalf("expected WorkflowError, got %T", err)
			}
			if !we.HasTag(types.TagZeroDivisionError) {
				t.Errorf("expected ZeroDivisionError tag, got %v", we.Tags)
			}
		})
	}
}

func TestListLiteral(t *testing.T) {
	scope := newTestScope()

	node, err := ParseExpression("[1, 2, 3]")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	got, err := Evaluate(node, scope)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if got.Type() != types.TypeList {
		t.Fatalf("expected list, got %s", got.Type())
	}
	if len(got.AsList()) != 3 {
		t.Errorf("expected 3 elements, got %d", len(got.AsList()))
	}
}

func TestParseValue(t *testing.T) {
	tests := []struct {
		name  string
		input interface{}
	}{
		{"nil", nil},
		{"bool_true", true},
		{"int", int64(42)},
		{"float", 3.14},
		{"string", "hello"},
		{"expression", "${1 + 2}"},
		{"list", []interface{}{int64(1), "two"}},
	}

	scope := newTestScope()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, err := ParseValue(tt.input)
			if err != nil {
				t.Fatalf("ParseValue error: %v", err)
			}
			_, err = Evaluate(node, scope)
			if err != nil && tt.name != "expression" {
				// expression might fail if variables not in scope
				t.Fatalf("eval error: %v", err)
			}
		})
	}
}

func TestParseValueExpression(t *testing.T) {
	scope := newTestScope()
	scope.vars["x"] = types.NewInt(10)

	node, err := ParseValue("${x + 5}")
	if err != nil {
		t.Fatalf("ParseValue error: %v", err)
	}
	got, err := Evaluate(node, scope)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if !got.Equal(types.NewInt(15)) {
		t.Errorf("got %v, want 15", got)
	}
}

func TestNegativeIndexRaisesError(t *testing.T) {
	scope := newTestScope()
	scope.vars["items"] = types.NewList([]types.Value{
		types.NewInt(10),
		types.NewInt(20),
		types.NewInt(30),
	})

	node, err := ParseExpression("items[-1]")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Evaluate(node, scope)
	if err == nil {
		t.Fatal("expected IndexError for negative index")
	}
	we, ok := err.(*types.WorkflowError)
	if !ok {
		t.Fatalf("expected WorkflowError, got %T", err)
	}
	if !we.HasTag(types.TagIndexError) {
		t.Errorf("expected IndexError tag, got %v", we.Tags)
	}
}

func TestIndexOutOfRange(t *testing.T) {
	scope := newTestScope()
	scope.vars["items"] = types.NewList([]types.Value{
		types.NewInt(1),
	})

	node, err := ParseExpression("items[5]")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Evaluate(node, scope)
	if err == nil {
		t.Fatal("expected IndexError")
	}
	we, ok := err.(*types.WorkflowError)
	if !ok {
		t.Fatalf("expected WorkflowError, got %T", err)
	}
	if !we.HasTag(types.TagIndexError) {
		t.Errorf("expected IndexError tag, got %v", we.Tags)
	}
}

func TestMissingMapKey(t *testing.T) {
	scope := newTestScope()
	m := types.NewOrderedMap()
	m.Set("a", types.NewInt(1))
	scope.vars["obj"] = types.NewMap(m)

	node, err := ParseExpression("obj.missing")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Evaluate(node, scope)
	if err == nil {
		t.Fatal("expected KeyError")
	}
	we, ok := err.(*types.WorkflowError)
	if !ok {
		t.Fatalf("expected WorkflowError, got %T", err)
	}
	if !we.HasTag(types.TagKeyError) {
		t.Errorf("expected KeyError tag, got %v", we.Tags)
	}
}

func TestDottedFunctionCall(t *testing.T) {
	scope := newTestScope()
	scope.funcs["math.max"] = func(args []types.Value) (types.Value, error) {
		if len(args) != 2 {
			return types.Null, fmt.Errorf("math.max expects 2 args")
		}
		a, _ := args[0].AsNumber()
		b, _ := args[1].AsNumber()
		if a > b {
			return args[0], nil
		}
		return args[1], nil
	}

	node, err := ParseExpression("math.max(3, 7)")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	got, err := Evaluate(node, scope)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if !got.Equal(types.NewInt(7)) {
		t.Errorf("got %v, want 7", got)
	}
}
