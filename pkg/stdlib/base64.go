package stdlib

import (
	"encoding/base64"
	"fmt"

	"github.com/lemonberrylabs/gcw-emulator/pkg/types"
)

// registerBase64 registers base64.* functions.
func (r *Registry) registerBase64() {
	r.Register("base64.decode", base64Decode)
	r.Register("base64.encode", base64Encode)
}

func base64Decode(args []types.Value) (types.Value, error) {
	if len(args) == 0 {
		return types.Null, fmt.Errorf("base64.decode requires an argument")
	}

	var input string
	if args[0].Type() == types.TypeMap {
		if v, ok := args[0].AsMap().Get("data"); ok && v.Type() == types.TypeString {
			input = v.AsString()
		} else {
			return types.Null, types.NewTypeError("base64.decode: data must be a string")
		}
	} else if args[0].Type() == types.TypeString {
		input = args[0].AsString()
	} else {
		return types.Null, types.NewTypeError("base64.decode requires a string argument")
	}

	decoded, err := base64.StdEncoding.DecodeString(input)
	if err != nil {
		// Try URL-safe encoding
		decoded, err = base64.URLEncoding.DecodeString(input)
		if err != nil {
			return types.Null, types.NewValueError(
				fmt.Sprintf("base64.decode: invalid base64: %v", err))
		}
	}

	// Return as bytes type. GCW base64.decode returns bytes.
	return types.NewBytes(decoded), nil
}

func base64Encode(args []types.Value) (types.Value, error) {
	if len(args) == 0 {
		return types.Null, fmt.Errorf("base64.encode requires an argument")
	}

	var data []byte
	if args[0].Type() == types.TypeMap {
		if v, ok := args[0].AsMap().Get("data"); ok {
			switch v.Type() {
			case types.TypeBytes:
				data = v.AsBytes()
			case types.TypeString:
				data = []byte(v.AsString())
			default:
				return types.Null, types.NewTypeError("base64.encode: data must be bytes or string")
			}
		} else {
			return types.Null, fmt.Errorf("base64.encode: missing 'data' argument")
		}
	} else if args[0].Type() == types.TypeBytes {
		data = args[0].AsBytes()
	} else if args[0].Type() == types.TypeString {
		data = []byte(args[0].AsString())
	} else {
		return types.Null, types.NewTypeError("base64.encode requires bytes or string argument")
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	return types.NewString(encoded), nil
}
