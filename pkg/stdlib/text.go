package stdlib

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/lemonberrylabs/gcw-emulator/pkg/types"
)

// registerText registers text.* functions.
func (r *Registry) registerText() {
	r.Register("text.decode", textDecode)
	r.Register("text.encode", textEncode)
	r.Register("text.find_all", textFindAll)
	r.Register("text.find_all_regex", textFindAllRegex)
	r.Register("text.match_regex", textMatchRegex)
	r.Register("text.replace_all", textReplaceAll)
	r.Register("text.replace_all_regex", textReplaceAllRegex)
	r.Register("text.split", textSplit)
	r.Register("text.substring", textSubstring)
	r.Register("text.to_lower", textToLower)
	r.Register("text.to_upper", textToUpper)
	r.Register("text.url_decode", textURLDecode)
	r.Register("text.url_encode", textURLEncode)
	r.Register("text.url_encode_plus", textURLEncodePlus)
}

func textDecode(args []types.Value) (types.Value, error) {
	if len(args) == 0 {
		return types.Null, fmt.Errorf("text.decode requires an argument")
	}
	var data []byte
	if args[0].Type() == types.TypeMap {
		if v, ok := args[0].AsMap().Get("data"); ok && v.Type() == types.TypeBytes {
			data = v.AsBytes()
		} else {
			return types.Null, types.NewTypeError("text.decode: data must be bytes")
		}
	} else if args[0].Type() == types.TypeBytes {
		data = args[0].AsBytes()
	} else {
		return types.Null, types.NewTypeError("text.decode requires bytes argument")
	}
	return types.NewString(string(data)), nil
}

func textEncode(args []types.Value) (types.Value, error) {
	if len(args) == 0 {
		return types.Null, fmt.Errorf("text.encode requires an argument")
	}
	var s string
	if args[0].Type() == types.TypeMap {
		if v, ok := args[0].AsMap().Get("data"); ok && v.Type() == types.TypeString {
			s = v.AsString()
		} else {
			return types.Null, types.NewTypeError("text.encode: data must be a string")
		}
	} else if args[0].Type() == types.TypeString {
		s = args[0].AsString()
	} else {
		return types.Null, types.NewTypeError("text.encode requires string argument")
	}
	return types.NewBytes([]byte(s)), nil
}

func textFindAll(args []types.Value) (types.Value, error) {
	if len(args) == 0 {
		return types.Null, fmt.Errorf("text.find_all requires source and substr arguments")
	}

	var source, substr string
	if args[0].Type() == types.TypeMap {
		m := args[0].AsMap()
		if v, ok := m.Get("source"); ok {
			source = v.AsString()
		}
		if v, ok := m.Get("substr"); ok {
			substr = v.AsString()
		}
	} else if len(args) >= 2 {
		source = args[0].AsString()
		substr = args[1].AsString()
	}

	var results []types.Value
	start := 0
	for {
		idx := strings.Index(source[start:], substr)
		if idx == -1 {
			break
		}
		matchMap := types.NewOrderedMap()
		matchMap.Set("index", types.NewInt(int64(start+idx)))
		matchMap.Set("match", types.NewString(substr))
		results = append(results, types.NewMap(matchMap))
		start += idx + 1
	}

	return types.NewList(results), nil
}

func textFindAllRegex(args []types.Value) (types.Value, error) {
	var source, pattern string
	if len(args) > 0 && args[0].Type() == types.TypeMap {
		m := args[0].AsMap()
		if v, ok := m.Get("source"); ok {
			source = v.AsString()
		}
		if v, ok := m.Get("pattern"); ok {
			pattern = v.AsString()
		}
	} else if len(args) >= 2 {
		source = args[0].AsString()
		pattern = args[1].AsString()
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return types.Null, types.NewValueError(fmt.Sprintf("invalid regex: %v", err))
	}

	matches := re.FindAllStringIndex(source, -1)
	result := make([]types.Value, len(matches))
	for i, m := range matches {
		matchMap := types.NewOrderedMap()
		matchMap.Set("index", types.NewInt(int64(m[0])))
		matchMap.Set("match", types.NewString(source[m[0]:m[1]]))
		result[i] = types.NewMap(matchMap)
	}

	return types.NewList(result), nil
}

func textMatchRegex(args []types.Value) (types.Value, error) {
	var source, pattern string
	if len(args) > 0 && args[0].Type() == types.TypeMap {
		m := args[0].AsMap()
		if v, ok := m.Get("source"); ok {
			source = v.AsString()
		}
		if v, ok := m.Get("regex"); ok {
			pattern = v.AsString()
		} else if v, ok := m.Get("pattern"); ok {
			pattern = v.AsString()
		}
	} else if len(args) >= 2 {
		source = args[0].AsString()
		pattern = args[1].AsString()
	}

	// GCW text.match_regex does a full-string match
	fullPattern := "^(?:" + pattern + ")$"
	re, err := regexp.Compile(fullPattern)
	if err != nil {
		return types.Null, types.NewValueError(fmt.Sprintf("invalid regex: %v", err))
	}

	return types.NewBool(re.MatchString(source)), nil
}

func textReplaceAll(args []types.Value) (types.Value, error) {
	var source, substr, replacement string
	if len(args) > 0 && args[0].Type() == types.TypeMap {
		m := args[0].AsMap()
		if v, ok := m.Get("source"); ok {
			source = v.AsString()
		}
		if v, ok := m.Get("substr"); ok {
			substr = v.AsString()
		}
		if v, ok := m.Get("replacement"); ok {
			replacement = v.AsString()
		}
	} else if len(args) >= 3 {
		source = args[0].AsString()
		substr = args[1].AsString()
		replacement = args[2].AsString()
	}

	return types.NewString(strings.ReplaceAll(source, substr, replacement)), nil
}

func textReplaceAllRegex(args []types.Value) (types.Value, error) {
	var source, pattern, replacement string
	if len(args) > 0 && args[0].Type() == types.TypeMap {
		m := args[0].AsMap()
		if v, ok := m.Get("source"); ok {
			source = v.AsString()
		}
		if v, ok := m.Get("pattern"); ok {
			pattern = v.AsString()
		}
		if v, ok := m.Get("replacement"); ok {
			replacement = v.AsString()
		}
	} else if len(args) >= 3 {
		source = args[0].AsString()
		pattern = args[1].AsString()
		replacement = args[2].AsString()
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return types.Null, types.NewValueError(fmt.Sprintf("invalid regex: %v", err))
	}

	return types.NewString(re.ReplaceAllString(source, replacement)), nil
}

func textSplit(args []types.Value) (types.Value, error) {
	var source, separator string
	if len(args) > 0 && args[0].Type() == types.TypeMap {
		m := args[0].AsMap()
		if v, ok := m.Get("source"); ok {
			source = v.AsString()
		}
		if v, ok := m.Get("separator"); ok {
			separator = v.AsString()
		}
	} else if len(args) >= 2 {
		source = args[0].AsString()
		separator = args[1].AsString()
	}

	parts := strings.Split(source, separator)
	result := make([]types.Value, len(parts))
	for i, p := range parts {
		result[i] = types.NewString(p)
	}
	return types.NewList(result), nil
}

func textSubstring(args []types.Value) (types.Value, error) {
	var source string
	var start, end int64

	if len(args) > 0 && args[0].Type() == types.TypeMap {
		m := args[0].AsMap()
		if v, ok := m.Get("source"); ok {
			source = v.AsString()
		}
		if v, ok := m.Get("start"); ok && v.Type() == types.TypeInt {
			start = v.AsInt()
		}
		if v, ok := m.Get("end"); ok && v.Type() == types.TypeInt {
			end = v.AsInt()
		} else {
			end = int64(len(source))
		}
	} else if len(args) >= 3 {
		source = args[0].AsString()
		start = args[1].AsInt()
		end = args[2].AsInt()
	} else if len(args) >= 2 {
		source = args[0].AsString()
		start = args[1].AsInt()
		end = int64(len(source))
	}

	if start < 0 {
		start = 0
	}
	if end > int64(len(source)) {
		end = int64(len(source))
	}
	if start > end {
		return types.NewString(""), nil
	}

	return types.NewString(source[start:end]), nil
}

func textToLower(args []types.Value) (types.Value, error) {
	if len(args) == 0 {
		return types.Null, fmt.Errorf("text.to_lower requires an argument")
	}
	var s string
	if args[0].Type() == types.TypeMap {
		if v, ok := args[0].AsMap().Get("source"); ok {
			s = v.AsString()
		}
	} else {
		s = args[0].AsString()
	}
	return types.NewString(strings.ToLower(s)), nil
}

func textToUpper(args []types.Value) (types.Value, error) {
	if len(args) == 0 {
		return types.Null, fmt.Errorf("text.to_upper requires an argument")
	}
	var s string
	if args[0].Type() == types.TypeMap {
		if v, ok := args[0].AsMap().Get("source"); ok {
			s = v.AsString()
		}
	} else {
		s = args[0].AsString()
	}
	return types.NewString(strings.ToUpper(s)), nil
}

func textURLDecode(args []types.Value) (types.Value, error) {
	if len(args) == 0 {
		return types.Null, fmt.Errorf("text.url_decode requires an argument")
	}
	var s string
	if args[0].Type() == types.TypeMap {
		if v, ok := args[0].AsMap().Get("data"); ok {
			s = v.AsString()
		}
	} else {
		s = args[0].AsString()
	}
	decoded, err := url.QueryUnescape(s)
	if err != nil {
		return types.Null, types.NewValueError(fmt.Sprintf("text.url_decode: %v", err))
	}
	return types.NewString(decoded), nil
}

func textURLEncode(args []types.Value) (types.Value, error) {
	if len(args) == 0 {
		return types.Null, fmt.Errorf("text.url_encode requires an argument")
	}
	var s string
	if args[0].Type() == types.TypeMap {
		if v, ok := args[0].AsMap().Get("data"); ok {
			s = v.AsString()
		}
	} else {
		s = args[0].AsString()
	}
	return types.NewString(url.PathEscape(s)), nil
}

func textURLEncodePlus(args []types.Value) (types.Value, error) {
	if len(args) == 0 {
		return types.Null, fmt.Errorf("text.url_encode_plus requires an argument")
	}
	var s string
	if args[0].Type() == types.TypeMap {
		if v, ok := args[0].AsMap().Get("data"); ok {
			s = v.AsString()
		}
	} else {
		s = args[0].AsString()
	}
	return types.NewString(url.QueryEscape(s)), nil
}
