package stdlib

import (
	"fmt"
	"math"
	"strconv"

	"github.com/lemonberrylabs/gcw-emulator/pkg/types"
)

// registerExpressionHelpers registers built-in expression helper functions:
// default, keys, len, type, int, double, string, bool.
func (r *Registry) registerExpressionHelpers() {
	r.Register("default", stdDefault)
	r.Register("keys", stdKeys)
	r.Register("len", stdLen)
	r.Register("type", stdType)
	r.Register("int", stdInt)
	r.Register("double", stdDouble)
	r.Register("string", stdString)
	r.Register("bool", stdBool)
}

func stdDefault(args []types.Value) (types.Value, error) {
	if err := requireArgs("default", args, 2, 2); err != nil {
		return types.Null, err
	}
	if args[0].IsNull() {
		return args[1], nil
	}
	return args[0], nil
}

func stdKeys(args []types.Value) (types.Value, error) {
	if err := requireArgs("keys", args, 1, 1); err != nil {
		return types.Null, err
	}
	if args[0].Type() != types.TypeMap {
		return types.Null, types.NewTypeError("keys() requires a map argument")
	}
	keys := args[0].AsMap().Keys()
	result := make([]types.Value, len(keys))
	for i, k := range keys {
		result[i] = types.NewString(k)
	}
	return types.NewList(result), nil
}

func stdLen(args []types.Value) (types.Value, error) {
	if err := requireArgs("len", args, 1, 1); err != nil {
		return types.Null, err
	}
	switch args[0].Type() {
	case types.TypeString:
		return types.NewInt(int64(len(args[0].AsString()))), nil
	case types.TypeList:
		return types.NewInt(int64(len(args[0].AsList()))), nil
	case types.TypeMap:
		return types.NewInt(int64(args[0].AsMap().Len())), nil
	case types.TypeBytes:
		return types.NewInt(int64(len(args[0].AsBytes()))), nil
	default:
		return types.Null, types.NewTypeError(
			fmt.Sprintf("len() not supported for %s", args[0].Type()))
	}
}

func stdType(args []types.Value) (types.Value, error) {
	if err := requireArgs("type", args, 1, 1); err != nil {
		return types.Null, err
	}
	return types.NewString(args[0].Type().String()), nil
}

func stdInt(args []types.Value) (types.Value, error) {
	if err := requireArgs("int", args, 1, 1); err != nil {
		return types.Null, err
	}
	v := args[0]
	switch v.Type() {
	case types.TypeInt:
		return v, nil
	case types.TypeDouble:
		return types.NewInt(int64(v.AsDouble())), nil
	case types.TypeString:
		i, err := strconv.ParseInt(v.AsString(), 10, 64)
		if err != nil {
			// Try parsing as float first
			f, ferr := strconv.ParseFloat(v.AsString(), 64)
			if ferr != nil {
				return types.Null, types.NewValueError(
					fmt.Sprintf("cannot convert %q to int", v.AsString()))
			}
			return types.NewInt(int64(f)), nil
		}
		return types.NewInt(i), nil
	case types.TypeBool:
		if v.AsBool() {
			return types.NewInt(1), nil
		}
		return types.NewInt(0), nil
	default:
		return types.Null, types.NewTypeError(
			fmt.Sprintf("cannot convert %s to int", v.Type()))
	}
}

func stdDouble(args []types.Value) (types.Value, error) {
	if err := requireArgs("double", args, 1, 1); err != nil {
		return types.Null, err
	}
	v := args[0]
	switch v.Type() {
	case types.TypeDouble:
		return v, nil
	case types.TypeInt:
		return types.NewDouble(float64(v.AsInt())), nil
	case types.TypeString:
		f, err := strconv.ParseFloat(v.AsString(), 64)
		if err != nil {
			return types.Null, types.NewValueError(
				fmt.Sprintf("cannot convert %q to double", v.AsString()))
		}
		return types.NewDouble(f), nil
	case types.TypeBool:
		if v.AsBool() {
			return types.NewDouble(1.0), nil
		}
		return types.NewDouble(0.0), nil
	default:
		return types.Null, types.NewTypeError(
			fmt.Sprintf("cannot convert %s to double", v.Type()))
	}
}

func stdString(args []types.Value) (types.Value, error) {
	if err := requireArgs("string", args, 1, 1); err != nil {
		return types.Null, err
	}
	return types.NewString(args[0].String()), nil
}

func stdBool(args []types.Value) (types.Value, error) {
	if err := requireArgs("bool", args, 1, 1); err != nil {
		return types.Null, err
	}
	v := args[0]
	switch v.Type() {
	case types.TypeBool:
		return v, nil
	case types.TypeInt:
		return types.NewBool(v.AsInt() != 0), nil
	case types.TypeDouble:
		return types.NewBool(v.AsDouble() != 0 && !math.IsNaN(v.AsDouble())), nil
	case types.TypeString:
		return types.NewBool(v.AsString() != ""), nil
	case types.TypeNull:
		return types.NewBool(false), nil
	default:
		return types.NewBool(true), nil
	}
}
