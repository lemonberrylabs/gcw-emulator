package stdlib

import (
	"fmt"
	"time"

	"github.com/lemonberrylabs/gcw-emulator/pkg/types"
)

// registerTime registers time.* functions.
func (r *Registry) registerTime() {
	r.Register("time.format", timeFormat)
	r.Register("time.parse", timeParse)
}

func timeFormat(args []types.Value) (types.Value, error) {
	if len(args) == 0 {
		return types.Null, fmt.Errorf("time.format requires a timestamp argument")
	}

	var timestamp float64
	var tz string = "UTC"

	if args[0].Type() == types.TypeMap {
		m := args[0].AsMap()
		if v, ok := m.Get("timestamp"); ok {
			n, ok := v.AsNumber()
			if !ok {
				return types.Null, types.NewTypeError("time.format: timestamp must be a number")
			}
			timestamp = n
		}
		if v, ok := m.Get("timezone"); ok && v.Type() == types.TypeString {
			tz = v.AsString()
		}
	} else {
		n, ok := args[0].AsNumber()
		if !ok {
			return types.Null, types.NewTypeError("time.format: timestamp must be a number")
		}
		timestamp = n
		if len(args) >= 2 && args[1].Type() == types.TypeString {
			tz = args[1].AsString()
		}
	}

	sec := int64(timestamp)
	nsec := int64((timestamp - float64(sec)) * 1e9)
	t := time.Unix(sec, nsec)

	loc, err := time.LoadLocation(tz)
	if err != nil {
		return types.Null, types.NewValueError(fmt.Sprintf("time.format: invalid timezone %q: %v", tz, err))
	}

	t = t.In(loc)
	return types.NewString(t.Format(time.RFC3339Nano)), nil
}

func timeParse(args []types.Value) (types.Value, error) {
	if len(args) == 0 {
		return types.Null, fmt.Errorf("time.parse requires a value argument")
	}

	var input string
	if args[0].Type() == types.TypeMap {
		if v, ok := args[0].AsMap().Get("value"); ok && v.Type() == types.TypeString {
			input = v.AsString()
		} else {
			return types.Null, types.NewTypeError("time.parse: value must be a string")
		}
	} else if args[0].Type() == types.TypeString {
		input = args[0].AsString()
	} else {
		return types.Null, types.NewTypeError("time.parse requires a string argument")
	}

	t, err := time.Parse(time.RFC3339, input)
	if err != nil {
		// Try RFC3339Nano
		t, err = time.Parse(time.RFC3339Nano, input)
		if err != nil {
			return types.Null, types.NewValueError(
				fmt.Sprintf("time.parse: invalid timestamp format %q: %v", input, err))
		}
	}

	return types.NewDouble(float64(t.Unix()) + float64(t.Nanosecond())/1e9), nil
}
