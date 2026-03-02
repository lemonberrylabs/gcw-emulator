package stdlib

import (
	"encoding/json"
	"fmt"

	"github.com/lemonberrylabs/gcw-emulator/pkg/types"
)

// registerJSON registers json.* functions.
func (r *Registry) registerJSON() {
	r.Register("json.decode", jsonDecode)
	r.Register("json.encode", jsonEncode)
	r.Register("json.encode_to_string", jsonEncodeToString)
}

func jsonDecode(args []types.Value) (types.Value, error) {
	if len(args) == 0 {
		return types.Null, fmt.Errorf("json.decode requires an argument")
	}

	var input string
	if args[0].Type() == types.TypeMap {
		if v, ok := args[0].AsMap().Get("data"); ok && v.Type() == types.TypeString {
			input = v.AsString()
		} else {
			return types.Null, types.NewTypeError("json.decode: data must be a string")
		}
	} else if args[0].Type() == types.TypeString {
		input = args[0].AsString()
	} else {
		return types.Null, types.NewTypeError("json.decode requires a string argument")
	}

	var raw interface{}
	if err := json.Unmarshal([]byte(input), &raw); err != nil {
		return types.Null, types.NewValueError(fmt.Sprintf("json.decode: invalid JSON: %v", err))
	}

	return types.ValueFromJSON(raw), nil
}

func jsonEncode(args []types.Value) (types.Value, error) {
	if len(args) == 0 {
		return types.Null, fmt.Errorf("json.encode requires an argument")
	}

	var val types.Value
	if args[0].Type() == types.TypeMap {
		if v, ok := args[0].AsMap().Get("data"); ok {
			val = v
		} else {
			val = args[0]
		}
	} else {
		val = args[0]
	}

	b, err := val.MarshalJSON()
	if err != nil {
		return types.Null, fmt.Errorf("json.encode: %v", err)
	}
	return types.NewBytes(b), nil
}

func jsonEncodeToString(args []types.Value) (types.Value, error) {
	if len(args) == 0 {
		return types.Null, fmt.Errorf("json.encode_to_string requires an argument")
	}

	var val types.Value
	if args[0].Type() == types.TypeMap {
		if v, ok := args[0].AsMap().Get("data"); ok {
			val = v
		} else {
			val = args[0]
		}
	} else {
		val = args[0]
	}

	b, err := val.MarshalJSON()
	if err != nil {
		return types.Null, fmt.Errorf("json.encode_to_string: %v", err)
	}
	return types.NewString(string(b)), nil
}
