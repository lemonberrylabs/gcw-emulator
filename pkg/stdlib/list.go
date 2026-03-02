package stdlib

import (
	"fmt"

	"github.com/lemonberrylabs/gcw-emulator/pkg/types"
)

// registerList registers list.* functions.
func (r *Registry) registerList() {
	r.Register("list.concat", listConcat)
	r.Register("list.prepend", listPrepend)
}

func listConcat(args []types.Value) (types.Value, error) {
	// list.concat(list1, list2) - concatenates two lists
	if len(args) == 0 {
		return types.NewList(nil), nil
	}

	var list1, list2 types.Value

	if args[0].Type() == types.TypeMap {
		m := args[0].AsMap()
		if l, ok := m.Get("list"); ok {
			list1 = l
			if e, ok := m.Get("element"); ok {
				list2 = e
			} else if e, ok := m.Get("value"); ok {
				list2 = e
			} else {
				return types.Null, fmt.Errorf("list.concat: missing second argument")
			}
		} else {
			return types.Null, fmt.Errorf("list.concat: missing 'list' argument")
		}
	} else if len(args) >= 2 {
		list1 = args[0]
		list2 = args[1]
	} else {
		return types.Null, fmt.Errorf("list.concat requires two arguments")
	}

	if list1.Type() != types.TypeList {
		return types.Null, types.NewTypeError("list.concat: first argument must be a list")
	}

	result := make([]types.Value, 0, len(list1.AsList())+1)
	result = append(result, list1.AsList()...)

	// If second argument is a list, concatenate; otherwise append as element
	if list2.Type() == types.TypeList {
		result = append(result, list2.AsList()...)
	} else {
		result = append(result, list2)
	}
	return types.NewList(result), nil
}

func listPrepend(args []types.Value) (types.Value, error) {
	if len(args) == 0 {
		return types.Null, fmt.Errorf("list.prepend requires arguments")
	}

	var list types.Value
	var value types.Value

	if args[0].Type() == types.TypeMap {
		m := args[0].AsMap()
		if l, ok := m.Get("list"); ok {
			list = l
		} else {
			return types.Null, fmt.Errorf("list.prepend: missing 'list' argument")
		}
		if v, ok := m.Get("value"); ok {
			value = v
		} else {
			return types.Null, fmt.Errorf("list.prepend: missing 'value' argument")
		}
	} else if len(args) >= 2 {
		list = args[0]
		value = args[1]
	} else {
		return types.Null, fmt.Errorf("list.prepend requires list and value arguments")
	}

	if list.Type() != types.TypeList {
		return types.Null, types.NewTypeError("list.prepend: first argument must be a list")
	}

	result := make([]types.Value, 0, len(list.AsList())+1)
	result = append(result, value)
	result = append(result, list.AsList()...)
	return types.NewList(result), nil
}
