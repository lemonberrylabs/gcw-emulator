package expr

import (
	"fmt"
	"math"
	"strings"

	"github.com/lemonberrylabs/gcw-emulator/pkg/types"
)

// Scope provides variable lookup and function resolution for expression evaluation.
type Scope interface {
	// GetVariable returns the value of a variable by name.
	GetVariable(name string) (types.Value, error)

	// CallFunction calls a named function with the given arguments.
	CallFunction(name string, args []types.Value) (types.Value, error)
}

// Evaluate evaluates an expression node within the given scope.
func Evaluate(node Node, scope Scope) (types.Value, error) {
	switch n := node.(type) {
	case *LiteralNode:
		return evalLiteral(n)
	case *IdentNode:
		return scope.GetVariable(n.Name)
	case *BinaryNode:
		return evalBinary(n, scope)
	case *UnaryNode:
		return evalUnary(n, scope)
	case *PropertyNode:
		return evalProperty(n, scope)
	case *IndexNode:
		return evalIndex(n, scope)
	case *CallNode:
		return evalCall(n, scope)
	case *ListNode:
		return evalList(n, scope)
	case *MapNode:
		return evalMap(n, scope)
	case *InNode:
		return evalIn(n, scope)
	case *StringInterpolation:
		return evalInterpolation(n, scope)
	default:
		return types.Null, fmt.Errorf("unsupported expression node type: %T", node)
	}
}

func evalLiteral(n *LiteralNode) (types.Value, error) {
	switch n.TokenType {
	case TokenNull:
		return types.Null, nil
	case TokenTrue:
		return types.NewBool(true), nil
	case TokenFalse:
		return types.NewBool(false), nil
	case TokenInt:
		return types.NewInt(n.IntVal), nil
	case TokenFloat:
		return types.NewDouble(n.FloatVal), nil
	case TokenString:
		return types.NewString(n.StrVal), nil
	default:
		return types.Null, fmt.Errorf("unknown literal type: %s", n.TokenType)
	}
}

func evalBinary(n *BinaryNode, scope Scope) (types.Value, error) {
	// Short-circuit for logical operators
	if n.Op == TokenAnd {
		left, err := Evaluate(n.Left, scope)
		if err != nil {
			return types.Null, err
		}
		if !left.Truthy() {
			return left, nil
		}
		return Evaluate(n.Right, scope)
	}
	if n.Op == TokenOr {
		left, err := Evaluate(n.Left, scope)
		if err != nil {
			return types.Null, err
		}
		if left.Truthy() {
			return left, nil
		}
		return Evaluate(n.Right, scope)
	}

	left, err := Evaluate(n.Left, scope)
	if err != nil {
		return types.Null, err
	}
	right, err := Evaluate(n.Right, scope)
	if err != nil {
		return types.Null, err
	}

	switch n.Op {
	case TokenPlus:
		return evalAdd(left, right)
	case TokenMinus:
		return evalArith(left, right, func(a, b int64) int64 { return a - b },
			func(a, b float64) float64 { return a - b })
	case TokenStar:
		return evalArith(left, right, func(a, b int64) int64 { return a * b },
			func(a, b float64) float64 { return a * b })
	case TokenSlash:
		return evalDivide(left, right)
	case TokenPercent:
		return evalModulo(left, right)
	case TokenIntDiv:
		return evalIntDivide(left, right)
	case TokenEq:
		return types.NewBool(left.Equal(right)), nil
	case TokenNeq:
		return types.NewBool(!left.Equal(right)), nil
	case TokenLt:
		return evalCompare(left, right, func(c int) bool { return c < 0 })
	case TokenGt:
		return evalCompare(left, right, func(c int) bool { return c > 0 })
	case TokenLte:
		return evalCompare(left, right, func(c int) bool { return c <= 0 })
	case TokenGte:
		return evalCompare(left, right, func(c int) bool { return c >= 0 })
	default:
		return types.Null, fmt.Errorf("unsupported binary operator: %s", n.Op)
	}
}

func evalAdd(left, right types.Value) (types.Value, error) {
	// String concatenation (string + string only; no auto-coercion per GCW spec)
	if left.Type() == types.TypeString && right.Type() == types.TypeString {
		return types.NewString(left.AsString() + right.AsString()), nil
	}
	// String + non-string is a TypeError in GCW
	if left.Type() == types.TypeString || right.Type() == types.TypeString {
		return types.Null, types.NewTypeError(
			fmt.Sprintf("unsupported operand types for +: %s and %s (explicit conversion required)", left.Type(), right.Type()))
	}
	// List concatenation
	if left.Type() == types.TypeList && right.Type() == types.TypeList {
		result := make([]types.Value, 0, len(left.AsList())+len(right.AsList()))
		result = append(result, left.AsList()...)
		result = append(result, right.AsList()...)
		return types.NewList(result), nil
	}
	return evalArith(left, right, func(a, b int64) int64 { return a + b },
		func(a, b float64) float64 { return a + b })
}

func evalArith(left, right types.Value, intOp func(int64, int64) int64, floatOp func(float64, float64) float64) (types.Value, error) {
	if left.Type() == types.TypeInt && right.Type() == types.TypeInt {
		return types.NewInt(intOp(left.AsInt(), right.AsInt())), nil
	}

	a, aOk := left.AsNumber()
	b, bOk := right.AsNumber()
	if !aOk || !bOk {
		return types.Null, types.NewTypeError(
			fmt.Sprintf("unsupported operand types: %s and %s", left.Type(), right.Type()))
	}

	return types.NewDouble(floatOp(a, b)), nil
}

func evalDivide(left, right types.Value) (types.Value, error) {
	a, aOk := left.AsNumber()
	b, bOk := right.AsNumber()
	if !aOk || !bOk {
		return types.Null, types.NewTypeError(
			fmt.Sprintf("unsupported operand types for /: %s and %s", left.Type(), right.Type()))
	}
	if b == 0 {
		return types.Null, types.NewZeroDivisionError()
	}
	// Division always returns double in GCW
	return types.NewDouble(a / b), nil
}

func evalModulo(left, right types.Value) (types.Value, error) {
	if left.Type() == types.TypeInt && right.Type() == types.TypeInt {
		if right.AsInt() == 0 {
			return types.Null, types.NewZeroDivisionError()
		}
		return types.NewInt(left.AsInt() % right.AsInt()), nil
	}

	a, aOk := left.AsNumber()
	b, bOk := right.AsNumber()
	if !aOk || !bOk {
		return types.Null, types.NewTypeError(
			fmt.Sprintf("unsupported operand types for %%: %s and %s", left.Type(), right.Type()))
	}
	if b == 0 {
		return types.Null, types.NewZeroDivisionError()
	}
	return types.NewDouble(math.Mod(a, b)), nil
}

func evalIntDivide(left, right types.Value) (types.Value, error) {
	if left.Type() == types.TypeInt && right.Type() == types.TypeInt {
		if right.AsInt() == 0 {
			return types.Null, types.NewZeroDivisionError()
		}
		return types.NewInt(left.AsInt() / right.AsInt()), nil
	}

	a, aOk := left.AsNumber()
	b, bOk := right.AsNumber()
	if !aOk || !bOk {
		return types.Null, types.NewTypeError(
			fmt.Sprintf("unsupported operand types for //: %s and %s", left.Type(), right.Type()))
	}
	if b == 0 {
		return types.Null, types.NewZeroDivisionError()
	}
	return types.NewInt(int64(math.Floor(a / b))), nil
}

func evalCompare(left, right types.Value, test func(int) bool) (types.Value, error) {
	cmp, err := compare(left, right)
	if err != nil {
		return types.Null, err
	}
	return types.NewBool(test(cmp)), nil
}

// compare returns negative, zero, or positive for ordering.
func compare(a, b types.Value) (int, error) {
	// Numbers
	if (a.Type() == types.TypeInt || a.Type() == types.TypeDouble) &&
		(b.Type() == types.TypeInt || b.Type() == types.TypeDouble) {
		an, _ := a.AsNumber()
		bn, _ := b.AsNumber()
		if an < bn {
			return -1, nil
		}
		if an > bn {
			return 1, nil
		}
		return 0, nil
	}

	// Strings
	if a.Type() == types.TypeString && b.Type() == types.TypeString {
		return strings.Compare(a.AsString(), b.AsString()), nil
	}

	return 0, types.NewTypeError(
		fmt.Sprintf("cannot compare %s and %s", a.Type(), b.Type()))
}

func evalUnary(n *UnaryNode, scope Scope) (types.Value, error) {
	operand, err := Evaluate(n.Operand, scope)
	if err != nil {
		return types.Null, err
	}

	switch n.Op {
	case TokenMinus:
		switch operand.Type() {
		case types.TypeInt:
			return types.NewInt(-operand.AsInt()), nil
		case types.TypeDouble:
			return types.NewDouble(-operand.AsDouble()), nil
		default:
			return types.Null, types.NewTypeError(
				fmt.Sprintf("unary minus not supported for %s", operand.Type()))
		}
	case TokenNot:
		return types.NewBool(!operand.Truthy()), nil
	default:
		return types.Null, fmt.Errorf("unsupported unary operator: %s", n.Op)
	}
}

func evalProperty(n *PropertyNode, scope Scope) (types.Value, error) {
	obj, err := Evaluate(n.Object, scope)
	if err != nil {
		return types.Null, err
	}

	if obj.Type() != types.TypeMap {
		return types.Null, types.NewTypeError(
			fmt.Sprintf("cannot access property '%s' on %s", n.Property, obj.Type()))
	}

	val, ok := obj.AsMap().Get(n.Property)
	if !ok {
		return types.Null, types.NewKeyError(
			fmt.Sprintf("key '%s' not found in map", n.Property))
	}
	return val, nil
}

func evalIndex(n *IndexNode, scope Scope) (types.Value, error) {
	obj, err := Evaluate(n.Object, scope)
	if err != nil {
		return types.Null, err
	}

	idx, err := Evaluate(n.Index, scope)
	if err != nil {
		return types.Null, err
	}

	switch obj.Type() {
	case types.TypeList:
		if idx.Type() != types.TypeInt {
			return types.Null, types.NewTypeError("list index must be an integer")
		}
		list := obj.AsList()
		i := int(idx.AsInt())
		// GCW does NOT support negative indices
		if i < 0 || i >= len(list) {
			return types.Null, types.NewIndexError(
				fmt.Sprintf("list index %d out of range (length %d)", idx.AsInt(), len(list)))
		}
		return list[i], nil

	case types.TypeMap:
		if idx.Type() != types.TypeString {
			return types.Null, types.NewTypeError("map key must be a string")
		}
		val, ok := obj.AsMap().Get(idx.AsString())
		if !ok {
			return types.Null, types.NewKeyError(
				fmt.Sprintf("key '%s' not found in map", idx.AsString()))
		}
		return val, nil

	case types.TypeString:
		if idx.Type() != types.TypeInt {
			return types.Null, types.NewTypeError("string index must be an integer")
		}
		s := obj.AsString()
		i := int(idx.AsInt())
		if i < 0 {
			i = len(s) + i
		}
		if i < 0 || i >= len(s) {
			return types.Null, types.NewIndexError(
				fmt.Sprintf("string index %d out of range (length %d)", idx.AsInt(), len(s)))
		}
		return types.NewString(string(s[i])), nil

	default:
		return types.Null, types.NewTypeError(
			fmt.Sprintf("cannot index %s", obj.Type()))
	}
}

func evalCall(n *CallNode, scope Scope) (types.Value, error) {
	// Build the function name from the node
	name := functionName(n.Function)

	// Evaluate arguments
	args := make([]types.Value, len(n.Args))
	for i, arg := range n.Args {
		val, err := Evaluate(arg, scope)
		if err != nil {
			return types.Null, err
		}
		args[i] = val
	}

	return scope.CallFunction(name, args)
}

// functionName extracts the dotted function name from a node.
func functionName(node Node) string {
	switch n := node.(type) {
	case *IdentNode:
		return n.Name
	case *PropertyNode:
		return functionName(n.Object) + "." + n.Property
	default:
		return ""
	}
}

func evalList(n *ListNode, scope Scope) (types.Value, error) {
	elements := make([]types.Value, len(n.Elements))
	for i, elem := range n.Elements {
		val, err := Evaluate(elem, scope)
		if err != nil {
			return types.Null, err
		}
		elements[i] = val
	}
	return types.NewList(elements), nil
}

func evalMap(n *MapNode, scope Scope) (types.Value, error) {
	m := types.NewOrderedMap()
	for i := range n.Keys {
		key, err := Evaluate(n.Keys[i], scope)
		if err != nil {
			return types.Null, err
		}
		if key.Type() != types.TypeString {
			return types.Null, types.NewTypeError("map key must be a string")
		}
		val, err := Evaluate(n.Values[i], scope)
		if err != nil {
			return types.Null, err
		}
		m.Set(key.AsString(), val)
	}
	return types.NewMap(m), nil
}

func evalIn(n *InNode, scope Scope) (types.Value, error) {
	val, err := Evaluate(n.Value, scope)
	if err != nil {
		return types.Null, err
	}
	container, err := Evaluate(n.Container, scope)
	if err != nil {
		return types.Null, err
	}

	var found bool
	switch container.Type() {
	case types.TypeList:
		for _, item := range container.AsList() {
			if val.Equal(item) {
				found = true
				break
			}
		}
	case types.TypeMap:
		if val.Type() != types.TypeString {
			return types.Null, types.NewTypeError("'in' on map requires string key")
		}
		_, found = container.AsMap().Get(val.AsString())
	case types.TypeString:
		if val.Type() != types.TypeString {
			return types.Null, types.NewTypeError("'in' on string requires string operand")
		}
		found = strings.Contains(container.AsString(), val.AsString())
	default:
		return types.Null, types.NewTypeError(
			fmt.Sprintf("'in' not supported for %s", container.Type()))
	}

	if n.Negated {
		found = !found
	}
	return types.NewBool(found), nil
}

func evalInterpolation(n *StringInterpolation, scope Scope) (types.Value, error) {
	var sb strings.Builder
	for _, part := range n.Parts {
		val, err := Evaluate(part, scope)
		if err != nil {
			return types.Null, err
		}
		sb.WriteString(val.String())
	}
	return types.NewString(sb.String()), nil
}
