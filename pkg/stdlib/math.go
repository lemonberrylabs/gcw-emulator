package stdlib

import (
	"math"

	"github.com/lemonberrylabs/gcw-emulator/pkg/types"
)

// registerMath registers math.* functions.
func (r *Registry) registerMath() {
	r.Register("math.abs", mathAbs)
	r.Register("math.floor", mathFloor)
	r.Register("math.max", mathMax)
	r.Register("math.min", mathMin)
}

func mathAbs(args []types.Value) (types.Value, error) {
	if err := requireArgs("math.abs", args, 1, 1); err != nil {
		return types.Null, err
	}
	v := args[0]
	switch v.Type() {
	case types.TypeInt:
		i := v.AsInt()
		if i < 0 {
			return types.NewInt(-i), nil
		}
		return v, nil
	case types.TypeDouble:
		return types.NewDouble(math.Abs(v.AsDouble())), nil
	default:
		return types.Null, types.NewTypeError("math.abs requires a number argument")
	}
}

func mathFloor(args []types.Value) (types.Value, error) {
	if err := requireArgs("math.floor", args, 1, 1); err != nil {
		return types.Null, err
	}
	v := args[0]
	switch v.Type() {
	case types.TypeInt:
		return v, nil
	case types.TypeDouble:
		return types.NewInt(int64(math.Floor(v.AsDouble()))), nil
	default:
		return types.Null, types.NewTypeError("math.floor requires a number argument")
	}
}

func mathMax(args []types.Value) (types.Value, error) {
	if err := requireArgs("math.max", args, 2, 2); err != nil {
		return types.Null, err
	}
	a, aOk := args[0].AsNumber()
	b, bOk := args[1].AsNumber()
	if !aOk || !bOk {
		return types.Null, types.NewTypeError("math.max requires number arguments")
	}
	if a >= b {
		return args[0], nil
	}
	return args[1], nil
}

func mathMin(args []types.Value) (types.Value, error) {
	if err := requireArgs("math.min", args, 2, 2); err != nil {
		return types.Null, err
	}
	a, aOk := args[0].AsNumber()
	b, bOk := args[1].AsNumber()
	if !aOk || !bOk {
		return types.Null, types.NewTypeError("math.min requires number arguments")
	}
	if a <= b {
		return args[0], nil
	}
	return args[1], nil
}
